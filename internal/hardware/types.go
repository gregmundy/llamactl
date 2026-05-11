package hardware

// Info is the structured snapshot the hardware command writes to
// ~/.config/llamactl/hardware.json. Every field is best-effort: when a
// detection probe fails, the zero value is preserved and Detect does not
// surface the underlying error (doctor's job, not hardware's).
type Info struct {
	Chip                string `json:"chip"`                  // "Apple M2 Pro"
	ChipGen             string `json:"chip_gen"`              // "M2"
	RAMBytes            uint64 `json:"ram_bytes"`             // from sysctl hw.memsize
	IogpuWiredLimitMB   int    `json:"iogpu_wired_limit_mb"`  // 0 = unset (uses default ~75% of RAM)
	HypervisorPresent   bool   `json:"hypervisor_present"`    // sysctl kern.hv_vmm_present == 1
	MetalDeviceDetected bool   `json:"metal_device_detected"` // system_profiler SPDisplaysDataType has Metal entry
	OSVersion           string `json:"os_version"`            // sw_vers -productVersion
}
