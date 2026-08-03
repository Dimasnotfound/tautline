package main

import (
	"reflect"
	"testing"
)

func TestTranslatePathArgumentsConvertsWindowsPathFlags(t *testing.T) {
	arguments := []string{
		"mcp",
		"--cookie-jar", `D:\PRIMACODES (ME)\chatgpt-mcp\runtime\v2\state\lightpanda\cookies.json`,
		"--storage-engine", "sqlite",
		"--storage-sqlite-path", `D:\PRIMACODES (ME)\chatgpt-mcp\runtime\v2\state\lightpanda\storage.sqlite`,
		"--http-cache-dir", `D:\PRIMACODES (ME)\chatgpt-mcp\runtime\v2\cache\lightpanda`,
		"--log-level", "error",
	}
	translated, err := translatePathArguments(arguments, func(path string) (string, error) {
		return "/mnt/d/" + path[3:], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"mcp",
		"--cookie-jar", `/mnt/d/PRIMACODES (ME)\chatgpt-mcp\runtime\v2\state\lightpanda\cookies.json`,
		"--storage-engine", "sqlite",
		"--storage-sqlite-path", `/mnt/d/PRIMACODES (ME)\chatgpt-mcp\runtime\v2\state\lightpanda\storage.sqlite`,
		"--http-cache-dir", `/mnt/d/PRIMACODES (ME)\chatgpt-mcp\runtime\v2\cache\lightpanda`,
		"--log-level", "error",
	}
	if !reflect.DeepEqual(translated, expected) {
		t.Fatalf("translated=%#v, want %#v", translated, expected)
	}
}

func TestTranslatePathArgumentsLeavesNonPathValuesAlone(t *testing.T) {
	arguments := []string{"mcp", "--storage-sqlite-path", ":memory:", "--cookie", "/home/user/cookies.json"}
	translated, err := translatePathArguments(arguments, func(path string) (string, error) {
		t.Fatalf("translator unexpectedly called for %q", path)
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(translated, arguments) {
		t.Fatalf("translated=%#v, want %#v", translated, arguments)
	}
}
