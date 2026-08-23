package prepare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const defaultAssetsBaseURL = "https://resources.download.minecraft.net"

type assetIndexDoc struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
}

func (p *Preparer) downloadAssets(ctx context.Context, root, indexID, indexURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("asset index %s: status %d", indexURL, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var index assetIndexDoc
	if err := json.Unmarshal(raw, &index); err != nil {
		return err
	}
	indexPath := filepath.Join(root, "assets", "indexes", indexID+".json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(indexPath, raw, 0o644); err != nil {
		return err
	}

	dl := &downloader{http: p.http, root: root, workers: p.workers}
	jobs := make([]job, 0, len(index.Objects))
	for _, object := range index.Objects {
		prefix := object.Hash[:2]
		jobs = append(jobs, job{
			url:  p.assetsBaseURL + "/" + prefix + "/" + object.Hash,
			path: "assets/objects/" + prefix + "/" + object.Hash,
			sha1: object.Hash,
		})
	}
	return dl.run(ctx, jobs, "Ресурсы (assets)")
}
