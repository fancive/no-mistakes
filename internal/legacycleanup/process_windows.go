//go:build windows

package legacycleanup

// A surviving PID record is uncertain on Windows without querying the task
// identity. Fail closed; the user must stop the old service and remove its PID
// record with the old binary before confirming cleanup.
func processAlive(pid int) bool { return pid > 0 }
