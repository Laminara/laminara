package maven

import (
	"fmt"
	"strings"
)

const defaultExtension = "jar"

func Path(coords string) (string, error) {
	extension := defaultExtension
	if at := strings.LastIndex(coords, "@"); at >= 0 {
		extension, coords = coords[at+1:], coords[:at]
	}
	parts := strings.Split(coords, ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid maven coordinates %q", coords)
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact, version := parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) >= 4 {
		file += "-" + parts[3]
	}
	return group + "/" + artifact + "/" + version + "/" + file + "." + extension, nil
}
