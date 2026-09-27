// msgboxcheck drives src/goa/examples/msgbox.exe through its UI and verifies
// what was actually displayed.
//
// A GUI program cannot be checked with the stdout golden files the other
// examples use, so instead of guessing we ask Windows itself: find the dialog
// by its (UTF-16) caption, read the text it is showing, click the Yes button,
// and confirm the program took the right branch and exits cleanly.
//
// Usage: msgboxcheck <path-to-msgbox.exe>
//
//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	BM_CLICK = 0x00F5
	WM_CLOSE = 0x0010
	IDYES    = 6
)

var user32 = syscall.NewLazyDLL("user32.dll")

var (
	pFindWindowW    = user32.NewProc("FindWindowW")
	pFindWindowExW  = user32.NewProc("FindWindowExW")
	pGetDesktopWin  = user32.NewProc("GetDesktopWindow")
	pGetWindowTextW = user32.NewProc("GetWindowTextW")
	pGetClassNameW  = user32.NewProc("GetClassNameW")
	pGetDlgItem     = user32.NewProc("GetDlgItem")
	pSendMessageW   = user32.NewProc("SendMessageW")
	pTextLenW       = user32.NewProc("GetWindowTextLengthW")
)

// u16 returns a pointer to a NUL-terminated UTF-16 copy of s.
func u16(s string) uintptr {
	buf := append(utf16.Encode([]rune(s)), 0)
	return uintptr(unsafe.Pointer(&buf[0]))
}

func findWindow(title string) uintptr {
	h, _, _ := pFindWindowW.Call(0, u16(title))
	return h
}

func waitWindow(title string, d time.Duration) uintptr {
	deadline := time.Now().Add(d)
	for {
		if h := findWindow(title); h != 0 {
			return h
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func winText(h uintptr) string {
	n, _, _ := pTextLenW.Call(h)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(n)+1)
	return string(utf16.Decode(buf[:n]))
}

func winClass(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := pGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return string(utf16.Decode(buf[:n]))
}

// dialogText returns the body text of a dialog. A message box has two "Static"
// children -- the first is the icon (empty), the second holds the text -- so
// take the longest rather than the first.
func dialogText(h uintptr) string {
	best := ""
	var prev uintptr
	for i := 0; i < 50; i++ {
		c, _, _ := pFindWindowExW.Call(h, prev, 0, 0)
		if c == 0 {
			break
		}
		if winClass(c) == "Static" {
			if t := winText(c); len(t) > len(best) {
				best = t
			}
		}
		prev = c
	}
	return best
}

func closeWindow(h uintptr) { pSendMessageW.Call(h, WM_CLOSE, 0, 0) }

// listTopLevel prints every top-level window (class + title). Handy when a
// dialog does not turn up where you expected it.
func listTopLevel() {
	desktop, _, _ := pGetDesktopWin.Call()
	fmt.Println("  top-level windows:")
	var prev uintptr
	for i := 0; i < 200; i++ {
		h, _, _ := pFindWindowExW.Call(desktop, prev, 0, 0)
		if h == 0 {
			break
		}
		fmt.Printf("    [%s] %q\n", winClass(h), winText(h))
		prev = h
	}
}

var failures int

func fatal(format string, a ...any) {
	fmt.Printf("  FAIL: %s\n", fmt.Sprintf(format, a...))
	failures++
	os.Exit(1)
}

// listChildren dumps the child windows of the top-level window with the given
// title, so you can see which control actually carries the body text.
func listChildren(title string) {
	h := findWindow(title)
	if h == 0 {
		fmt.Printf("  no top-level window titled %q\n", title)
		return
	}
	fmt.Printf("  children of %q:\n", title)
	var prev uintptr
	for i := 0; i < 50; i++ {
		c, _, _ := pFindWindowExW.Call(h, prev, 0, 0)
		if c == 0 {
			break
		}
		fmt.Printf("    [%s] %q\n", winClass(c), winText(c))
		prev = c
	}
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: msgboxcheck <exe>")
	}
	exe := os.Args[1]
	if len(os.Args) > 2 && os.Args[2] == "-list" {
		listTopLevel()
		return
	}
	if len(os.Args) > 3 && os.Args[2] == "-kids" {
		listChildren(os.Args[3])
		return
	}

	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		fatal("cannot start %s: %v", exe, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Never leave a modal dialog stuck on screen, whatever happens.
	defer func() {
		for _, t := range []string{"goa GUI 演示", "结果"} {
			if h := findWindow(t); h != 0 {
				closeWindow(h)
			}
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	// 1. The first dialog must appear with our UTF-16 caption. Matching the
	//    title is itself the proof that the wide strings were encoded right.
	dlg := waitWindow("goa GUI 演示", 10*time.Second)
	if dlg == 0 {
		select {
		case err := <-done:
			fatal("process exited before showing a dialog: %v", err)
		default:
		}
		listTopLevel()
		fatal("first dialog never appeared (is the desktop interactive?)")
	}
	txt1 := dialogText(dlg)
	fmt.Printf("  dialog 1 caption = %q\n", winText(dlg))
	fmt.Printf("  dialog 1 text    = %q\n", txt1)
	if !strings.Contains(txt1, "没用 gcc") {
		fatal("dialog 1 body text looks wrong: %q", txt1)
	}

	// 2. Click "Yes" (the button carries the IDYES control id).
	btn, _, _ := pGetDlgItem.Call(dlg, IDYES)
	if btn == 0 {
		fatal("no Yes button (GetDlgItem IDYES) in the dialog")
	}
	pSendMessageW.Call(btn, BM_CLICK, 0, 0)

	// 3. The program should now take the IDYES branch and show a second box.
	dlg2 := waitWindow("结果", 5*time.Second)
	if dlg2 == 0 {
		fatal("second dialog never appeared after clicking Yes")
	}
	txt2 := dialogText(dlg2)
	fmt.Printf("  dialog 2 caption = %q\n", winText(dlg2))
	fmt.Printf("  dialog 2 text    = %q\n", txt2)
	if !strings.Contains(txt2, "IDYES") {
		fatal("expected the IDYES branch, got: %q", txt2)
	}

	// 4. Closing it must let the process exit cleanly.
	closeWindow(dlg2)
	select {
	case err := <-done:
		if err != nil {
			fatal("process exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		fatal("process did not exit after the dialog was closed")
	}
	fmt.Println("  msgbox: PASS (user32 import, MessageBoxW, UTF-16 text, IDYES branch)")
}
