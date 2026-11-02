package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/buildspec"
	"github.com/spf13/cobra"
)

func loadBuildSpec(path string) (*buildspec.Spec, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	spec, err := buildspec.Decode(data)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	for i := range spec.Files {
		entry := &spec.Files[i]
		if entry.Source == "" {
			entry.Source = entry.Path
		}
		info, err := root.Lstat(entry.Source)
		if err != nil {
			return nil, nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > buildspec.MaxFileSize {
			return nil, nil, errors.New("source должен быть обычным файлом до 512 МиБ")
		}
		file, err := root.Open(entry.Source)
		if err != nil {
			return nil, nil, err
		}
		hash := sha256.New()
		size, copyErr := io.Copy(hash, io.LimitReader(file, buildspec.MaxFileSize+1))
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, nil, err
		}
		digest := fmt.Sprintf("%x", hash.Sum(nil))
		if entry.SHA256 != "" && entry.SHA256 != digest {
			return nil, nil, fmt.Errorf("source %s не совпадает с закреплённым SHA256", entry.Source)
		}
		entry.SHA256 = digest
		entry.Size = uint64(size)
	}
	if err := spec.Validate(); err != nil {
		return nil, nil, err
	}
	canonical, _, err := spec.Canonical()
	return spec, canonical, err
}

func buildCmd() *cobra.Command {
	root := &cobra.Command{Use: "build", Short: "декларативная сборка из JSON: validate, plan, push, apply"}
	for _, action := range []string{"validate", "plan", "push", "apply"} {
		var expected, key string
		var detach, asJSON bool
		cmd := &cobra.Command{Use: action + " specification.json", Short: map[string]string{"validate": "проверить описание и локальные файлы", "plan": "показать ревизию, изменение и недостающие хеши", "push": "загрузить недостающие файлы без публикации", "apply": "подготовить и атомарно опубликовать желаемую сборку"}[action], Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			spec, data, err := loadBuildSpec(args[0])
			if err != nil {
				return err
			}
			if action == "validate" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return err
			}
			client := adminClient(cmd.Context())
			plan, err := client.PlanBuild(cmd.Context(), connect.NewRequest(&adminv1.PlanBuildRequest{SpecificationJson: data}))
			if err != nil {
				return err
			}
			if action == "plan" {
				return writeProto(cmd.OutOrStdout(), plan.Msg)
			}
			if expected != "" && expected != plan.Msg.Revision && !plan.Msg.NoOp {
				return errors.New("ревизия не совпадает с сохранённым планом")
			}
			if plan.Msg.NoOp {
				return writeProto(cmd.OutOrStdout(), plan.Msg)
			}
			if err := pushBuildFiles(cmd, filepath.Dir(args[0]), spec, plan.Msg.MissingHashes); err != nil {
				return err
			}
			if action == "push" {
				return writeProto(cmd.OutOrStdout(), plan.Msg)
			}
			response, err := client.StartJob(cmd.Context(), connect.NewRequest(&adminv1.StartJobRequest{Arguments: []string{"build-apply", string(data), plan.Msg.Revision}, IdempotencyKey: key}))
			if err != nil {
				return err
			}
			if detach {
				return writeProto(cmd.OutOrStdout(), response.Msg.Job)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Задание:", response.Msg.Job.Id)
			if err := watchJob(cmd, response.Msg.Job.Id, 0, asJSON); err != nil {
				return err
			}
			verified, err := client.PlanBuild(cmd.Context(), connect.NewRequest(&adminv1.PlanBuildRequest{SpecificationJson: data}))
			if err != nil {
				return err
			}
			if !verified.Msg.NoOp {
				return errors.New("после apply сервер не подтвердил желаемую ревизию")
			}
			return nil
		}}
		if action == "apply" {
			cmd.Flags().StringVar(&expected, "revision", "", "ревизия ранее полученного plan")
			cmd.Flags().StringVar(&key, "key", "", "ключ идемпотентности задания CI")
			cmd.Flags().BoolVar(&detach, "detach", false, "вернуть ID задания без ожидания")
			cmd.Flags().BoolVar(&asJSON, "json", false, "события выполнения в NDJSON")
		}
		root.AddCommand(cmd)
	}
	return root
}

func pushBuildFiles(cmd *cobra.Command, dir string, spec *buildspec.Spec, missing []string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	client := adminClient(cmd.Context())
	for _, digest := range missing {
		var entry buildspec.File
		for _, candidate := range spec.Files {
			if candidate.SHA256 == digest {
				entry = candidate
				break
			}
		}
		if entry.SHA256 == "" {
			return errors.New("сервер запросил хеш вне описания сборки")
		}
		file, err := root.Open(entry.Source)
		if err != nil {
			return err
		}
		err = func() error {
			defer file.Close()
			offset := uint64(0)
			buffer := make([]byte, 1<<20)
			for {
				if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
					return err
				}
				n, err := io.ReadFull(file, buffer[:min(uint64(len(buffer)), entry.Size-offset)])
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
					return err
				}
				response, err := client.UploadBuildChunk(cmd.Context(), connect.NewRequest(&adminv1.UploadBuildChunkRequest{Sha256: digest, Size: entry.Size, Offset: offset, Data: buffer[:n]}))
				if err != nil {
					return err
				}
				if response.Msg.Complete {
					if response.Msg.Offset != entry.Size {
						return errors.New("сервер подтвердил неверный размер")
					}
					return nil
				}
				if response.Msg.Offset > entry.Size || response.Msg.Offset == offset {
					return errors.New("загрузка не продвигается; проверьте source и повторите push")
				}
				offset = response.Msg.Offset
			}
		}()
		if err != nil {
			return err
		}
	}
	return nil
}
