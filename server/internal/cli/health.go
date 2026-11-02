package cli

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func healthCmd() *cobra.Command {
	var endpoint string
	var timeout time.Duration
	var live bool
	cmd := &cobra.Command{
		Use: "health", Short: "проверить готовность HTTP сервера", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/readyz"
			if live {
				path = "/healthz"
			}
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, strings.TrimRight(endpoint, "/")+path, nil)
			if err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("timeout должен быть положительным")
			}
			client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if _, err := io.Copy(cmd.OutOrStdout(), io.LimitReader(resp.Body, 64<<10)); err != nil {
				return err
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("проверка готовности: HTTP %d", resp.StatusCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "url", "http://127.0.0.1:8099", "HTTP адрес сервера")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "срок ожидания ответа")
	cmd.Flags().BoolVar(&live, "live", false, "проверить только работу процесса")
	return cmd
}
