package tui

// formatStatus returns a color-coded status string
func formatStatus(status string) string {
	switch status {
	case "RUNNING":
		return "[green]RUNNING[-]"
	case "STOPPED", "TERMINATED":
		return "[red]" + status + "[-]"
	case "PROVISIONING", "STAGING", "STOPPING":
		return "[yellow]" + status + "[-]"
	case "SUSPENDING", "SUSPENDED":
		return "[orange]" + status + "[-]"
	default:
		return "[gray]" + status + "[-]"
	}
}
