package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/atomicfile"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/controlauth"
	"github.com/laminara/laminara/server/internal/progress"
	"google.golang.org/protobuf/proto"
)

const maxConcurrent = 4

type record struct {
	Info       *adminv1.JobInfo `json:"info"`
	Owner      string           `json:"owner"`
	Key        string           `json:"key"`
	Digest     string           `json:"digest"`
	cancel     context.CancelFunc
	writeError error
	logBytes   int64
	changed    chan struct{}
}

type Manager struct {
	ctx      context.Context
	dir      string
	registry *command.Registry
	active   func(controlauth.Token) bool
	mu       sync.Mutex
	jobs     map[string]*record
	running  int
	closing  bool
	workers  sync.WaitGroup
}

func New(ctx context.Context, dir string, registry *command.Registry, active func(controlauth.Token) bool) (*Manager, error) {
	if dir == "" {
		return nil, errors.New("не задан каталог заданий")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{ctx: ctx, dir: dir, registry: registry, active: active, jobs: map[string]*record{}}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, errors.New("неверный файл задания")
		}
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			return nil, err
		}
		var job record
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, errors.New("повреждена запись задания")
		}
		if job.Info == nil || !safeID(job.Info.Id) || file.Name() != job.Info.Id+".json" {
			return nil, errors.New("неверная запись задания")
		}
		job.changed = make(chan struct{})
		if info, err := os.Stat(m.logPath(job.Info.Id)); err == nil {
			job.logBytes = info.Size()
		}
		m.jobs[job.Info.Id] = &job
		if job.Info.State == adminv1.JobState_JOB_STATE_RUNNING {
			events, err := m.readEvents(job.Info.Id, 0)
			if err != nil {
				return nil, err
			}
			job.Info.State = adminv1.JobState_JOB_STATE_INTERRUPTED
			job.Info.ExitCode = 1
			for _, event := range events {
				job.Info.LastSequence = max(job.Info.LastSequence, event.Sequence)
				if result := event.Event.GetResult(); result != nil {
					job.Info.ExitCode = result.ExitCode
					job.Info.State = adminv1.JobState_JOB_STATE_FAILED
					if result.ExitCode == 0 {
						job.Info.State = adminv1.JobState_JOB_STATE_SUCCEEDED
					}
				}
			}
			job.Info.FinishedAtUnixNanos = time.Now().UnixNano()
			if err := m.save(&job); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

func safeID(id string) bool {
	if len(id) < 26 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z') && !(r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

func (m *Manager) save(job *record) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(m.dir, job.Info.Id+".json"), data, 0o600)
}

func (m *Manager) Start(ctx context.Context, request *adminv1.StartJobRequest) (*adminv1.JobInfo, error) {
	if len(request.Arguments) == 0 || len(command.Format(request.Arguments...)) > 64<<10 || len(request.IdempotencyKey) > 256 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("нужна команда не длиннее 64 КиБ и ключ не длиннее 256 символов"))
	}
	registered, ok := m.registry.Lookup(request.Arguments[0])
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("неизвестная команда"))
	}
	if registered.Secret {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("вывод этой команды содержит секреты; выполните её через exec без сохранения журнала задания"))
	}
	if err := m.registry.Authorize(ctx, registered.Name); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	actor := controlauth.Principal(ctx)
	if m.active != nil && !m.active(actor) {
		return nil, connect.NewError(connect.CodeUnauthenticated, controlauth.ErrUnauthorized)
	}
	args := slices.Clone(request.Arguments)
	args[0] = registered.Name
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(command.Format(args...))))
	key := request.IdempotencyKey
	if key == "" {
		key = rand.Text()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("сервер останавливается"))
	}
	if err := m.ctx.Err(); err != nil {
		return nil, err
	}
	for _, job := range m.jobs {
		if job.Owner != actor.ID || job.Key != key {
			continue
		}
		if job.Digest != digest {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("этот ключ уже использован с другой командой"))
		}
		return proto.Clone(job.Info).(*adminv1.JobInfo), nil
	}
	if m.running >= maxConcurrent {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("уже выполняются четыре задания; дождитесь завершения"))
	}
	if len(m.jobs) >= 10000 {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("история содержит 10000 заданий; архивируйте завершённые записи"))
	}
	work, cancel := context.WithCancel(controlauth.WithPrincipal(m.ctx, actor))
	job := &record{Info: &adminv1.JobInfo{Id: rand.Text(), Command: registered.Name, State: adminv1.JobState_JOB_STATE_RUNNING, CreatedAtUnixNanos: time.Now().UnixNano()}, Owner: actor.ID, Key: key, Digest: digest, cancel: cancel, changed: make(chan struct{})}
	if err := m.save(job); err != nil {
		cancel()
		return nil, err
	}
	m.jobs[job.Info.Id] = job
	m.running++
	m.workers.Go(func() { m.run(work, job, args, actor) })
	return proto.Clone(job.Info).(*adminv1.JobInfo), nil
}

func (m *Manager) run(ctx context.Context, job *record, args []string, actor controlauth.Token) {
	defer job.cancel()
	if m.active != nil && actor.ID != "local" {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if !m.active(actor) {
						job.cancel()
						return
					}
				}
			}
		}()
	}
	writer := &outputWriter{manager: m, job: job}
	err := m.registry.DispatchArgs(progress.With(ctx, writer), args, writer)
	if err != nil {
		_, _ = writer.Write([]byte(err.Error() + "\n"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Info.State = adminv1.JobState_JOB_STATE_SUCCEEDED
	if err != nil || job.writeError != nil {
		job.Info.State = adminv1.JobState_JOB_STATE_FAILED
		job.Info.ExitCode = 1
	}
	if ctx.Err() != nil {
		job.Info.State = adminv1.JobState_JOB_STATE_CANCELLED
		job.Info.ExitCode = 1
	}
	job.Info.FinishedAtUnixNanos = time.Now().UnixNano()
	result := &adminv1.ExecResponse{Event: &adminv1.ExecResponse_Result{Result: &adminv1.ExecResult{ExitCode: job.Info.ExitCode}}}
	if err := m.appendEvent(job, result); err != nil {
		job.writeError = err
		job.Info.State = adminv1.JobState_JOB_STATE_FAILED
		job.Info.ExitCode = 1
	}
	if err := m.save(job); err != nil {
		job.writeError = err
		job.Info.State = adminv1.JobState_JOB_STATE_FAILED
		job.Info.ExitCode = 1
	}
	close(job.changed)
	job.changed = make(chan struct{})
	m.running--
}

func (m *Manager) owned(ctx context.Context, id string) (*record, error) {
	job, ok := m.jobs[id]
	actor := controlauth.Principal(ctx)
	if !ok || (job.Owner != actor.ID && !actor.Allows("admin")) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("задание не найдено"))
	}
	return job, nil
}

func (m *Manager) Get(ctx context.Context, id string) (*adminv1.JobInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.owned(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.writeError != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("не удалось сохранить результат задания"))
	}
	return proto.Clone(job.Info).(*adminv1.JobInfo), nil
}

func (m *Manager) List(ctx context.Context) ([]*adminv1.JobInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*adminv1.JobInfo
	for id := range m.jobs {
		if job, err := m.owned(ctx, id); err == nil {
			result = append(result, proto.Clone(job.Info).(*adminv1.JobInfo))
		}
	}
	slices.SortFunc(result, func(a, b *adminv1.JobInfo) int {
		if a.CreatedAtUnixNanos > b.CreatedAtUnixNanos {
			return -1
		}
		if a.CreatedAtUnixNanos < b.CreatedAtUnixNanos {
			return 1
		}
		return 0
	})
	return result, nil
}

func (m *Manager) Cancel(ctx context.Context, id string) (*adminv1.JobInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.owned(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Info.State == adminv1.JobState_JOB_STATE_RUNNING {
		job.Info.CancelRequested = true
		if err := m.save(job); err != nil {
			return nil, err
		}
		job.cancel()
	}
	return proto.Clone(job.Info).(*adminv1.JobInfo), nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closing = true
	for _, job := range m.jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
