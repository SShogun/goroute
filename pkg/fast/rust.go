package fast

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	once          sync.Once
	rustAvailable bool
	rustVersion   string

	fnVersion     func() uintptr
	fnCompress    func(input uintptr, level int32) uintptr
	fnCountTokens func(input uintptr) int32
	fnFreeString  func(ptr uintptr)
)

func findRustLib() string {
	candidates := []string{
		"crates/goroute-fast/target/release/libgoroute_fast.so",
		"crates/goroute-fast/target/debug/libgoroute_fast.so",
		"/usr/local/lib/libgoroute_fast.so",
		"/usr/lib/libgoroute_fast.so",
	}

	// Also check relative to executable or home
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "libgoroute_fast.so"))
		candidates = append(candidates, filepath.Join(exeDir, "..", "crates", "goroute-fast", "target", "release", "libgoroute_fast.so"))
	}

	home, _ := os.UserHomeDir()
	candidates = append(candidates, filepath.Join(home, ".goroute", "libgoroute_fast.so"))
	candidates = append(candidates, filepath.Join(home, ".local", "lib", "libgoroute_fast.so"))
	candidates = append(candidates, filepath.Join(home, "Projects", "goroute", "crates", "goroute-fast", "target", "release", "libgoroute_fast.so"))

	for _, path := range candidates {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	return ""
}

func initRust() {
	libPath := findRustLib()
	if libPath == "" {
		rustAvailable = false
		return
	}

	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		rustAvailable = false
		return
	}

	purego.RegisterLibFunc(&fnVersion, lib, "goroute_rust_version")
	purego.RegisterLibFunc(&fnCompress, lib, "goroute_rust_compress")
	purego.RegisterLibFunc(&fnCountTokens, lib, "goroute_rust_count_tokens")
	purego.RegisterLibFunc(&fnFreeString, lib, "goroute_rust_free_string")

	if fnVersion != nil {
		vPtr := fnVersion()
		rustVersion = goString(vPtr)
		rustAvailable = true
	}
}

func goString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var bytes []byte
	p := (*byte)(unsafe.Pointer(ptr))
	for *p != 0 {
		bytes = append(bytes, *p)
		ptr++
		p = (*byte)(unsafe.Pointer(ptr))
	}
	return string(bytes)
}

func cString(s string) ([]byte, uintptr) {
	bytes := append([]byte(s), 0)
	return bytes, uintptr(unsafe.Pointer(&bytes[0]))
}

func IsAvailable() bool {
	once.Do(initRust)
	return rustAvailable
}

func Version() string {
	once.Do(initRust)
	if !rustAvailable {
		return "Rust Accelerator not loaded (using pure Go)"
	}
	return rustVersion
}

func Compress(input string, level int32) (string, bool) {
	once.Do(initRust)
	if !rustAvailable || fnCompress == nil || fnFreeString == nil {
		return input, false
	}

	_, inPtr := cString(input)
	outPtr := fnCompress(inPtr, level)
	if outPtr == 0 {
		return input, false
	}
	defer fnFreeString(outPtr)

	return goString(outPtr), true
}

func CountTokens(input string) (int, bool) {
	once.Do(initRust)
	if !rustAvailable || fnCountTokens == nil {
		return 0, false
	}

	_, inPtr := cString(input)
	count := fnCountTokens(inPtr)
	return int(count), true
}

func Status() map[string]interface{} {
	once.Do(initRust)
	return map[string]interface{}{
		"rust_available": rustAvailable,
		"rust_version":   Version(),
		"engine":         fmt.Sprintf("Hybrid Architecture (Go Core + Rust SIMD Engine)"),
	}
}
