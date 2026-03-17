package tunnel

import (
	"bufio"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

type Tunnel struct {
	cmd  *exec.Cmd
	URL  string
	done chan struct{}
}

func Start(port int, cfToken string) (*Tunnel, error) {
	args := []string{"tunnel"}
	if cfToken != "" {
		args = append(args, "run", "--token", cfToken)
	} else {
		args = append(args, "--url", fmt.Sprintf("http://localhost:%d", port))
	}

	cmd := exec.Command("cloudflared", args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}

	t := &Tunnel{
		cmd:  cmd,
		done: make(chan struct{}),
	}

	urlCh := make(chan string, 1)

	go func() {
		defer close(t.done)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			slog.Debug("cloudflared", "output", line)
			if strings.Contains(line, ".trycloudflare.com") {
				for word := range strings.FieldsSeq(line) {
					if strings.Contains(word, ".trycloudflare.com") {
						word = strings.TrimPrefix(word, "https://")
						word = strings.TrimPrefix(word, "http://")
						select {
						case urlCh <- word:
						default:
						}
						break
					}
				}
			}
		}
	}()

	if cfToken == "" {
		select {
		case url := <-urlCh:
			t.URL = url
		case <-t.done:
			return nil, fmt.Errorf("cloudflared exited before providing URL")
		}
	}

	return t, nil
}

func (t *Tunnel) Stop() {
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		<-t.done
	}
}
