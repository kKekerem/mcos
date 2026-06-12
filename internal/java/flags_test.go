package java

import (
	"strings"
	"testing"
)

func TestJVMArgsDefault(t *testing.T) {
	args := JVMArgs(ProfileDefault, 2048, 17)
	if len(args) != 2 || args[0] != "-Xms2048M" || args[1] != "-Xmx2048M" {
		t.Fatalf("default args = %v", args)
	}
}

func TestJVMArgsAikar(t *testing.T) {
	args := JVMArgs(ProfileAikar, 4096, 17)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-Xms4096M", "-Xmx4096M", "-XX:+UseG1GC", "-XX:G1HeapRegionSize=8M", "aikars.new.flags=true"} {
		if !strings.Contains(joined, want) {
			t.Errorf("aikar args missing %q in %v", want, args)
		}
	}
}

func TestJVMArgsAikarLargeHeap(t *testing.T) {
	args := JVMArgs(ProfileAikar, 16*1024, 21)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-XX:G1HeapRegionSize=16M") || !strings.Contains(joined, "-XX:G1NewSizePercent=40") {
		t.Errorf("large-heap aikar args wrong: %v", args)
	}
}

func TestJVMArgsTurbo(t *testing.T) {
	args := JVMArgs(ProfileTurbo, 8192, 21)
	joined := strings.Join(args, " ")
	// Turbo builds on Aikar...
	for _, want := range []string{"-Xmx8192M", "-XX:+UseG1GC", "aikars.new.flags=true"} {
		if !strings.Contains(joined, want) {
			t.Errorf("turbo args missing aikar base %q in %v", want, args)
		}
	}
	// ...and adds the throughput extras.
	for _, want := range []string{"-XX:+UseNUMA", "-XX:ParallelGCThreads=", "-XX:ConcGCThreads="} {
		if !strings.Contains(joined, want) {
			t.Errorf("turbo args missing %q in %v", want, args)
		}
	}
}

func TestJVMArgsJava8DropsExtras(t *testing.T) {
	args := JVMArgs(ProfileAikar, 2048, 8)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "G1MixedGCLiveThresholdPercent") {
		t.Errorf("java 8 should drop G1MixedGCLiveThresholdPercent: %v", args)
	}
}

func TestMajorFromVersion(t *testing.T) {
	cases := map[string]int{
		`openjdk version "21.0.5" 2024-10-15`:  21,
		`java version "1.8.0_402"`:             8,
		`openjdk version "17.0.10" 2024-01-16`: 17,
		`openjdk version "11.0.22" 2024-01-16`: 11,
		`garbage with no version`:              0,
	}
	for line, want := range cases {
		if got := majorFromVersion(line); got != want {
			t.Errorf("majorFromVersion(%q) = %d, want %d", line, got, want)
		}
	}
}
