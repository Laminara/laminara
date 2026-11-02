package cli

import (
	"fmt"
	"io"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/control"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func writeProto(out io.Writer, message proto.Message) error {
	data, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func jobsCmd() *cobra.Command {
	root := &cobra.Command{Use: "jobs", Short: "фоновые задания: запуск, журнал, состояние и отмена"}
	var key string
	var detach, asJSON bool
	run := &cobra.Command{Use: "run [ключи] -- команда [аргументы…]", Short: "запустить команду с сохранением результата", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client := adminClient(cmd.Context())
		response, err := client.StartJob(cmd.Context(), connect.NewRequest(&adminv1.StartJobRequest{Arguments: args, IdempotencyKey: key}))
		if err != nil {
			return err
		}
		if detach {
			return writeProto(cmd.OutOrStdout(), response.Msg.Job)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Задание:", response.Msg.Job.Id)
		return watchJob(cmd, response.Msg.Job.Id, 0, asJSON)
	}}
	run.Flags().StringVar(&key, "key", "", "ключ повторного запроса; одинаковая команда с тем же ключом запускается один раз")
	run.Flags().BoolVar(&detach, "detach", false, "вернуть JSON с ID без ожидания")
	run.Flags().BoolVar(&asJSON, "json", false, "события журнала в NDJSON")
	var after uint64
	var watchJSON bool
	watch := &cobra.Command{Use: "watch ID", Short: "продолжить чтение журнала задания", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return watchJob(cmd, args[0], after, watchJSON) }}
	watch.Flags().Uint64Var(&after, "after", 0, "последний полученный sequence")
	watch.Flags().BoolVar(&watchJSON, "json", false, "события журнала в NDJSON")
	root.AddCommand(run, watch,
		&cobra.Command{Use: "list", Short: "список заданий в JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := adminClient(cmd.Context()).ListJobs(cmd.Context(), connect.NewRequest(&adminv1.ListJobsRequest{}))
			if err != nil {
				return err
			}
			return writeProto(cmd.OutOrStdout(), response.Msg)
		}},
		&cobra.Command{Use: "get ID", Short: "состояние задания в JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			response, err := adminClient(cmd.Context()).GetJob(cmd.Context(), connect.NewRequest(&adminv1.GetJobRequest{Id: args[0]}))
			if err != nil {
				return err
			}
			return writeProto(cmd.OutOrStdout(), response.Msg.Job)
		}},
		&cobra.Command{Use: "cancel ID", Short: "запросить отмену задания", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			response, err := adminClient(cmd.Context()).CancelJob(cmd.Context(), connect.NewRequest(&adminv1.CancelJobRequest{Id: args[0]}))
			if err != nil {
				return err
			}
			return writeProto(cmd.OutOrStdout(), response.Msg.Job)
		}},
	)
	return root
}

func watchJob(cmd *cobra.Command, id string, after uint64, asJSON bool) error {
	job, err := control.WatchJob(cmd.Context(), adminClient(cmd.Context()), id, after, func(event *adminv1.StreamJobResponse) error {
		if asJSON {
			return writeProto(cmd.OutOrStdout(), event)
		}
		if output := event.Event.GetOutput(); output != nil {
			target := cmd.OutOrStdout()
			if output.Stderr {
				target = cmd.ErrOrStderr()
			}
			_, err := fmt.Fprint(target, output.Text)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if job.State != adminv1.JobState_JOB_STATE_SUCCEEDED {
		return fmt.Errorf("задание %s: %s, код %d", id, job.State, job.ExitCode)
	}
	return nil
}

func buildsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "builds", Short: "сборки и ревизии их настроек", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		response, err := adminClient(cmd.Context()).ListBuilds(cmd.Context(), connect.NewRequest(&adminv1.ListBuildsRequest{}))
		if err != nil {
			return err
		}
		if asJSON {
			return writeProto(cmd.OutOrStdout(), response.Msg)
		}
		for _, build := range response.Msg.Builds {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", build.Name, build.Status, build.SettingsRevision)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "вывести JSON")
	return cmd
}
