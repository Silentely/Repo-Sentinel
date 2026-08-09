package cli

import "fmt"

func (r Runner) runVersion(args []string) error {
	if len(args) != 0 {
		return newCLIError("version 不接受额外参数。")
	}
	info := r.dependencies.BuildInfo()
	_, err := fmt.Fprintf(
		r.stdout,
		"version=%s\ngit_sha=%s\ngit_branch=%s\nbuild_time=%s\nbuild_channel=%s\ngo_version=%s\nrepository=https://github.com/Silentely/Repo-Sentinel\n",
		info.Version,
		info.GitSHA,
		info.GitBranch,
		info.BuildTime,
		info.BuildChannel,
		info.GoVersion,
	)
	return err
}
