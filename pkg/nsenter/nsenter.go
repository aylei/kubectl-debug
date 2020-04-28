package nsenter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
)

// MountNSEnter is the client used to enter the mount namespace
type MountNSEnter struct {
	Target     int64  // target PID (required)
	MountLxcfs bool   // enter mount namespace or not
	MountFile  string // Mount namespace location, default to /proc/PID/ns/mnt
}

// Execute runs the given command with a default background context
func (cli *MountNSEnter) Execute(command string, args ...string) (stdout, stderr string, err error) {
	return cli.ExecuteContext(context.Background(), command, args...)
}

// ExecuteContext the given command using the specific nsenter config
func (cli *MountNSEnter) ExecuteContext(ctx context.Context, command string, args ...string) (string, string, error) {
	cmd, err := cli.setCommand(ctx)
	if err != nil {
		return "", "", fmt.Errorf("Error when set command: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Args = append(cmd.Args, command)
	cmd.Args = append(cmd.Args, args...)

	err = cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("Error while executing command: %v", err)
	}

	return stdout.String(), stderr.String(), nil
}

func (cli *MountNSEnter) setCommand(ctx context.Context) (*exec.Cmd, error) {
	if cli.Target == 0 {
		return nil, fmt.Errorf("Target must be specified")
	}
	var args []string
	args = append(args, "--target", strconv.FormatInt(cli.Target, 10))

	if cli.MountLxcfs {
		if cli.MountFile != "" {
			args = append(args, fmt.Sprintf("--mount=%s", cli.MountFile))
		} else {
			args = append(args, "--mount")
		}
	}

	cmd := exec.CommandContext(ctx, "/usr/bin/nsenter", args...)
	return cmd, nil
}
