package linux

import (
	"context"
	"strings"
	"time"

	"panel/internal/sshx"
)

func Detect(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (OSRelease, error) {
	res, err := exec.Exec(ctx, target, sshx.CommandSpec{Command: "cat /etc/os-release", Timeout: 10 * time.Second})
	if err != nil {
		return OSRelease{}, err
	}
	info := ParseOSRelease(res.Stdout)
	info.Supported = Supported(info)
	return info, nil
}

func ParseOSRelease(content string) OSRelease {
	values := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		values[parts[0]] = strings.Trim(parts[1], `"`)
	}
	return OSRelease{ID: values["ID"], VersionID: values["VERSION_ID"], PrettyName: values["PRETTY_NAME"]}
}
