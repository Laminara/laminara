package jobs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/progress"
	"google.golang.org/protobuf/encoding/protojson"
)

const maxLogBytes = 8 << 20

func (m *Manager) logPath(id string) string { return filepath.Join(m.dir, id+".events") }

func (m *Manager) appendEvent(job *record, event *adminv1.ExecResponse) error {
	if job.logBytes >= maxLogBytes && event.GetResult() == nil {
		job.Info.OutputTruncated = true
		return nil
	}
	message := &adminv1.StreamJobResponse{Sequence: job.Info.LastSequence + 1, Event: event}
	data, err := protojson.Marshal(message)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(m.logPath(job.Info.Id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	job.Info.LastSequence = message.Sequence
	job.logBytes += int64(len(data) + 1)
	close(job.changed)
	job.changed = make(chan struct{})
	return nil
}

func (m *Manager) readEvents(id string, after uint64) ([]*adminv1.StreamJobResponse, error) {
	file, err := os.Open(m.logPath(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxLogBytes+1<<20 {
		return nil, errors.New("неверный журнал задания")
	}
	reader := bufio.NewReader(io.LimitReader(file, maxLogBytes+1<<20))
	var result []*adminv1.StreamJobResponse
	var sequence uint64
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		event := &adminv1.StreamJobResponse{}
		if err := protojson.Unmarshal(bytes.TrimSpace(line), event); err != nil || event.Sequence != sequence+1 || event.Event == nil {
			return nil, errors.New("повреждён журнал задания")
		}
		sequence = event.Sequence
		if event.Sequence > after {
			result = append(result, event)
		}
	}
	return result, nil
}

func (m *Manager) Stream(ctx context.Context, id string, after uint64, send func(*adminv1.StreamJobResponse) error) error {
	for {
		m.mu.Lock()
		job, err := m.owned(ctx, id)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		if after > job.Info.LastSequence {
			m.mu.Unlock()
			return connect.NewError(connect.CodeInvalidArgument, errors.New("курсор находится за концом журнала"))
		}
		if job.writeError != nil {
			m.mu.Unlock()
			return connect.NewError(connect.CodeInternal, errors.New("не удалось сохранить журнал задания"))
		}
		events, err := m.readEvents(id, after)
		finished, changed := job.Info.State != adminv1.JobState_JOB_STATE_RUNNING, job.changed
		m.mu.Unlock()
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := send(event); err != nil {
				return err
			}
			after = event.Sequence
		}
		if finished {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

type outputWriter struct {
	manager      *Manager
	job          *record
	lastProgress time.Time
}

func (w *outputWriter) Write(data []byte) (int, error) {
	total := len(data)
	w.manager.mu.Lock()
	defer w.manager.mu.Unlock()
	for len(data) > 0 {
		end := min(len(data), 16<<10)
		if end < len(data) {
			for end > 0 && !utf8.RuneStart(data[end]) {
				end--
			}
		}
		if end == 0 {
			end = min(len(data), 16<<10)
		}
		event := &adminv1.ExecResponse{Event: &adminv1.ExecResponse_Output{Output: &adminv1.ExecOutputChunk{Text: string(data[:end])}}}
		if err := w.manager.appendEvent(w.job, event); err != nil {
			w.job.writeError = err
			return total - len(data), err
		}
		data = data[end:]
	}
	return total, nil
}

func (w *outputWriter) Report(event progress.Event) {
	w.manager.mu.Lock()
	defer w.manager.mu.Unlock()
	if time.Since(w.lastProgress) < 250*time.Millisecond && event.Current != event.Total {
		return
	}
	w.lastProgress = time.Now()
	message := &adminv1.ExecResponse{Event: &adminv1.ExecResponse_Progress{Progress: &adminv1.ExecProgress{Phase: event.Phase, Current: event.Current, Total: event.Total, Message: event.Message}}}
	if err := w.manager.appendEvent(w.job, message); err != nil {
		w.job.writeError = err
	}
}
