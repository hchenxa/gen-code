// Package proc provides cross-platform helpers for managing spawned child
// processes. On Unix the child becomes the leader of a new process group, so
// signals can target the leader together with descendants that inherit the
// group; descendants that call setsid (daemons, double-forks) escape the
// group and are not reached. On Windows the helpers terminate only the
// direct child via Process.Kill — grandchildren are not reaped, because the
// package does not yet use Job Objects.
package proc
