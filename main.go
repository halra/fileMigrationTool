package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Simplified function names and parameters for clarity.
func printOutput(prefix string, reader io.Reader, transferredFiles *int, sourceFiles int, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasSuffix(line, "/") {
			*transferredFiles++
			fmt.Printf("\r%s: %s", prefix, line) // Simplified output for clarity.
		}
	}
	fmt.Println() // Ensure output ends with newline.
}

func countFilesAndSizeSSH(user, host, dirPath string) (int, int64, error) {
	// Combined SSH command for efficiency.
	cmdStr := fmt.Sprintf("ssh %s@%s \"find %s -type f | wc -l && du -sb %s | cut -f1\"", user, host, dirPath, dirPath)
	cmd := exec.Command("/bin/bash", "-c", cmdStr)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	// Parse output for file count and size.
	var count int
	var size int64
	outputStr := strings.TrimSpace(string(output))
	fmt.Sscanf(outputStr, "%d\n%d", &count, &size)
	return count, size, nil
}

func updatePodSizePeriodically(pod, kubectlCmd, namespace, targetFolder string, podSize *int64, done chan bool) {
	ticker := time.NewTicker(15 * time.Second) // Example: make this configurable via CLI.
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Simplified command to only get directory size.
			cmdStr := fmt.Sprintf("%s exec %s -n %s -- du -sb %s | cut -f1", kubectlCmd, pod, namespace, targetFolder)
			cmd := exec.Command("/bin/bash", "-c", cmdStr)
			output, err := cmd.Output()
			if err == nil {
				var size int64
				fmt.Sscanf(string(output), "%d", &size)
				*podSize = size
			}
		case <-done:
			return
		}
	}
}

func main() {
	// Use more descriptive variable names and simplify flag setup.
	var (
		user, host, pod, namespace, sourceFolder, targetFolder, kubectlCmd string
		printFolderSize                                                    bool
	)
	flag.StringVar(&user, "user", "", "SSH username")
	flag.StringVar(&host, "host", "", "SSH host")
	flag.StringVar(&pod, "pod", "", "Target pod name")
	flag.StringVar(&namespace, "namespace", "", "Kubernetes namespace")
	flag.StringVar(&sourceFolder, "source", "", "Source folder for tar")
	flag.StringVar(&targetFolder, "target", "", "Target folder in pod for tar")
	flag.BoolVar(&printFolderSize, "debug", false, "Print folder size for debugging")
	flag.StringVar(&kubectlCmd, "kubectlCmd", "kubectl", "Custom kubectl command")
	flag.Parse()

	// Improved error handling and validation.
	if user == "" || host == "" || pod == "" || namespace == "" || sourceFolder == "" || targetFolder == "" {
		fmt.Println("Missing required arguments.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Initial file count and size from source via SSH.
	sourceCount, sourceSize, err := countFilesAndSizeSSH(user, host, sourceFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error counting source files: %v\n", err)
		os.Exit(1)
	}

	// Command execution with error handling simplified.
	sshCmd := fmt.Sprintf("ssh %s@%s \"tar -C %s -cf - .\" | %s exec -i %s -n %s -- tar -C %s -xf -",
		user, host, sourceFolder, kubectlCmd, pod, namespace, targetFolder)
	cmd := exec.Command("/bin/bash", "-c", sshCmd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var wg sync.WaitGroup
	wg.Add(2)
	var transferredFiles int
	var podSize int64

	go printOutput("STDOUT", stdout, &transferredFiles, sourceCount, &wg)
	go printOutput("STDERR", stderr, &transferredFiles, sourceCount, &wg)

	done := make(chan bool)
	if printFolderSize {
		go updatePodSizePeriodically(pod, kubectlCmd, namespace, targetFolder, &podSize, done)
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Command start error: %v\n", err)
		os.Exit(1)
	}

	if err := cmd.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "Command execution error: %v\n", err)
	}
	close(done)
	wg.Wait()

	// Final status output simplified for brevity.
	fmt.Printf("Transferred %d files. Source size: %d bytes. Pod size after transfer: %d bytes.\n", transferredFiles, sourceSize, podSize)
}
