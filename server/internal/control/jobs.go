package control

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

func WatchJob(ctx context.Context, client adminv1connect.AdminServiceClient, id string, after uint64, receive func(*adminv1.StreamJobResponse) error) (*adminv1.JobInfo, error) {
	for attempt := 0; ; attempt++ {
		stream, err := client.StreamJob(ctx, connect.NewRequest(&adminv1.StreamJobRequest{Id: id, AfterSequence: after}))
		if err == nil {
			for stream.Receive() {
				event := stream.Msg()
				if event.Sequence <= after {
					continue
				}
				if event.Sequence != after+1 {
					stream.Close()
					return nil, errors.New("пропущено событие задания")
				}
				if err := receive(event); err != nil {
					stream.Close()
					return nil, err
				}
				after = event.Sequence
				attempt = 0
			}
			err = stream.Err()
			stream.Close()
			if err == nil {
				response, getErr := client.GetJob(ctx, connect.NewRequest(&adminv1.GetJobRequest{Id: id}))
				err = getErr
				if err == nil && response.Msg.Job.State != adminv1.JobState_JOB_STATE_RUNNING {
					return response.Msg.Job, nil
				}
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil && (connect.CodeOf(err) != connect.CodeUnavailable && connect.CodeOf(err) != connect.CodeDeadlineExceeded) {
			return nil, err
		}
		if attempt >= 5 {
			return nil, fmt.Errorf("не удалось восстановить соединение; продолжите через jobs watch %s", id)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(1<<min(attempt, 3)) * time.Second):
		}
	}
}

func Execute(ctx context.Context, client adminv1connect.AdminServiceClient, args []string, started func(string), receive func(*adminv1.ExecResponse) error) (int32, error) {
	status, err := client.Status(ctx, connect.NewRequest(&adminv1.StatusRequest{}))
	if err != nil {
		return 0, err
	}
	useJob := slices.Contains(status.Msg.Capabilities, adminv1.AdminCapability_ADMIN_CAPABILITY_JOBS)
	if useJob {
		commands, err := client.ListCommands(ctx, connect.NewRequest(&adminv1.ListCommandsRequest{}))
		if err != nil {
			return 0, err
		}
		for _, command := range commands.Msg.Commands {
			if len(args) > 0 && (command.Name == args[0] || slices.Contains(command.Aliases, args[0])) && command.SecretOutput {
				useJob = false
				break
			}
		}
	}
	if useJob {
		request := &adminv1.StartJobRequest{Arguments: args, IdempotencyKey: rand.Text()}
		response, err := client.StartJob(ctx, connect.NewRequest(request))
		if err != nil && connect.CodeOf(err) == connect.CodeUnavailable {
			response, err = client.StartJob(ctx, connect.NewRequest(request))
		}
		if err != nil {
			return 0, err
		}
		if started != nil {
			started(response.Msg.Job.Id)
		}
		job, err := WatchJob(ctx, client, response.Msg.Job.Id, 0, func(event *adminv1.StreamJobResponse) error { return receive(event.Event) })
		if err != nil {
			return 0, err
		}
		if job.State == adminv1.JobState_JOB_STATE_INTERRUPTED {
			return 1, errors.New("сервер перезапущен во время задания; повторный запуск требует нового ключа")
		}
		return job.ExitCode, nil
	}
	if !slices.Contains(status.Msg.Capabilities, adminv1.AdminCapability_ADMIN_CAPABILITY_STRUCTURED_ARGUMENTS) {
		return 0, errors.New("запущенный сервер не поддерживает эту операцию клиента; обновите и перезапустите сервер")
	}
	stream, err := client.Exec(ctx, connect.NewRequest(&adminv1.ExecRequest{Arguments: args}))
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	var result *adminv1.ExecResult
	for stream.Receive() {
		if err := receive(stream.Msg()); err != nil {
			return 0, err
		}
		if value := stream.Msg().GetResult(); value != nil {
			result = value
		}
	}
	if err := stream.Err(); err != nil {
		return 0, err
	}
	if result == nil {
		return 0, errors.New("соединение закрыто без результата команды")
	}
	return result.ExitCode, nil
}
