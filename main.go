package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/urfave/cli/v2"
)

const (
	maxProxyChecks  = 5
	proxyFormatHelp = "добавьте прокси в одном из форматов: IP:PORT, IP:PORT:USER:PASS или USER:PASS@IP:PORT"
)

var (
	success int64
	failed  int64
)

func normalizeProxy(raw string) (string, error) {
	proxy := strings.TrimSpace(raw)
	if proxy == "" {
		return "", nil
	}

	if strings.Contains(proxy, "://") {
		parsed, err := url.Parse(proxy)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "", fmt.Errorf(proxyFormatHelp)
		}
		return proxy, nil
	}

	if strings.Contains(proxy, "@") {
		credentials, address, ok := strings.Cut(proxy, "@")
		if !ok || !hasTwoNonEmptyParts(credentials, ":") || !hasTwoNonEmptyParts(address, ":") {
			return "", fmt.Errorf(proxyFormatHelp)
		}
		return "socks5://" + proxy, nil
	}

	parts := strings.Split(proxy, ":")
	switch len(parts) {
	case 2:
		if hasOnlyNonEmptyParts(parts) {
			return "socks5://" + proxy, nil
		}
	case 4:
		if hasOnlyNonEmptyParts(parts) {
			ip, port, user, password := parts[0], parts[1], parts[2], parts[3]
			return fmt.Sprintf("socks5://%s:%s@%s:%s", user, password, ip, port), nil
		}
	}

	return "", fmt.Errorf(proxyFormatHelp)
}

func hasTwoNonEmptyParts(value string, separator string) bool {
	parts := strings.Split(value, separator)
	return len(parts) == 2 && hasOnlyNonEmptyParts(parts)
}

func hasOnlyNonEmptyParts(parts []string) bool {
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func readProxies(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var proxies []string
	sc := bufio.NewScanner(file)
	lineNumber := 0
	for sc.Scan() {
		lineNumber++
		line, err := normalizeProxy(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if line != "" {
			proxies = append(proxies, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("proxy file is empty")
	}

	return proxies, nil
}

func readLinks(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var links []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			links = append(links, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("links file is empty")
	}

	return links, nil
}

func selectProxySampleIndexes(total int, maxChecks int, rng *rand.Rand) []int {
	if total <= 0 || maxChecks <= 0 {
		return nil
	}

	checks := maxChecks
	if total < checks {
		checks = total
	}

	perm := rng.Perm(total)
	return perm[:checks]
}

func validateProxies(ctx context.Context, proxies []string) (float64, error) {
	indexes := selectProxySampleIndexes(len(proxies), maxProxyChecks, rand.New(rand.NewSource(time.Now().UnixNano())))
	if len(indexes) == 0 {
		return 0, fmt.Errorf("proxy list is empty")
	}

	var firstSpeed float64
	for i, index := range indexes {
		speed, err := testProxySpeed(ctx, proxies[index])
		if err != nil {
			return 0, fmt.Errorf("proxy #%d failed validation: %w", index+1, err)
		}
		if i == 0 {
			firstSpeed = speed
		}
	}

	return firstSpeed, nil
}

func testProxySpeed(ctx context.Context, rawProxy string) (float64, error) {
	proxyURL, err := normalizeProxy(rawProxy)
	if err != nil {
		return 0, err
	}
	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return 0, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(parsedURL),
		},
		Timeout: 20 * time.Second,
	}

	// Пробуем скачать маленький файл
	testFile := "https://proof.ovh.net/files/1Mb.dat"

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testFile, nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	written, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return 0, err
	}

	duration := time.Since(start).Seconds()
	if duration < 0.1 {
		duration = 0.1
	} // защита от деления на 0

	speedMBps := float64(written) / 1024 / 1024 / duration
	return speedMBps, nil
}

func calculateWorkers(speedMBps float64, videoSizeMB float64) int {
	if speedMBps <= 0 || videoSizeMB <= 0 {
		return 1
	}
	targetTime := 10.0 // Целевое время в секундах (чуть больше для медленных прокси)
	workers := (speedMBps * targetTime) / videoSizeMB
	w := int(workers)
	if w < 1 {
		w = 1
	}
	if w > 20 {
		w = 20
	} // Лимит безопасности
	return w
}

func worker(ctx context.Context, id int, urls <-chan string, proxies []string, idx *int32, delay time.Duration, outDir string, wg *sync.WaitGroup, bar *progressbar.ProgressBar) {
	defer wg.Done()

	for urlLink := range urls {
		select {
		case <-ctx.Done():
			return
		default:
		}

		i := atomic.AddInt32(idx, 1)
		proxy := proxies[int(i)%len(proxies)]

		cmd := exec.CommandContext(ctx, "yt-dlp",
			"--quiet",
			"-f", "bestvideo*+bestaudio/best",
			"--merge-output-format", "mp4",
			"-o", filepath.Join(outDir, "%(id)s.%(ext)s"),
			"--proxy", proxy,
			"--socket-timeout", "20",
			"--retries", "2",
			"--concurrent-fragments", "4",
			"--no-overwrites",
			"--no-playlist",
			"--newline",
			"--no-check-certificates",
			urlLink,
		)
		configureCommand(cmd)

		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard

		if err := cmd.Run(); err != nil {
			atomic.AddInt64(&failed, 1)
		} else {
			atomic.AddInt64(&success, 1)
		}

		_ = bar.Add(1)

		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

func runDownloader(c *cli.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		<-interrupts
		cancel()
	}()

	atomic.StoreInt64(&success, 0)
	atomic.StoreInt64(&failed, 0)

	links, err := readLinks(c.String("links"))
	if err != nil {
		return fmt.Errorf("links: %w", err)
	}

	proxies, err := readProxies(c.String("proxies"))
	if err != nil {
		return fmt.Errorf("proxies: %w", err)
	}

	fmt.Printf("🧪 Проверяю прокси: до %d случайных из %d...\n", maxProxyChecks, len(proxies))
	proxySpeed, err := validateProxies(ctx, proxies)
	if err != nil {
		return fmt.Errorf("прокси не рабочие: %w", err)
	}
	fmt.Printf("✅ Прокси прошли проверку. Скорость: %.2f МБ/с\n", proxySpeed)

	workers := c.Int("workers")
	if workers == 0 {
		workers = calculateWorkers(proxySpeed, c.Float64("size"))
		fmt.Printf("🔧 Авто-расчёт: %d воркеров (видео ~%.1f МБ)\n", workers, c.Float64("size"))
	} else {
		fmt.Printf("⚙️ Ручной режим: %d воркеров\n", workers)
	}

	outDir := c.String("output")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	urls := make(chan string, workers)
	go func() {
		defer close(urls)
		for _, link := range links {
			select {
			case <-ctx.Done():
				return
			case urls <- link:
			}
		}
	}()

	bar := progressbar.Default(int64(len(links)))

	var wg sync.WaitGroup
	var proxyIdx int32 = -1

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(ctx, i, urls, proxies, &proxyIdx, c.Duration("delay"), outDir, &wg, bar)
	}

	wg.Wait()
	fmt.Printf("\n🏁 Итог: ✅ %d | ❌ %d\n", atomic.LoadInt64(&success), atomic.LoadInt64(&failed))
	if ctx.Err() != nil {
		fmt.Println("⏹️ Завершено по сигналу пользователя")
	}
	return nil
}

func updateYTDLP(_ *cli.Context) error {
	cmd := exec.Command("yt-dlp", "-U")
	configureCommand(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	app := &cli.App{
		Name:  "download-tiktok",
		Usage: "Многопоточная CLI-утилита для скачивания видео через yt-dlp",
		Commands: []*cli.Command{
			{
				Name:   "update",
				Usage:  "обновить yt-dlp через yt-dlp -U",
				Action: updateYTDLP,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "links",
				Value: "links.txt",
				Usage: "путь к файлу со ссылками",
			},
			&cli.StringFlag{
				Name:  "proxies",
				Value: "proxies.txt",
				Usage: "путь к файлу с прокси",
			},
			&cli.StringFlag{
				Name:  "output",
				Value: "./downloads",
				Usage: "директория для сохранения видео",
			},
			&cli.IntFlag{
				Name:  "workers",
				Value: 0,
				Usage: "количество потоков (0 = автоопределение)",
			},
			&cli.Float64Flag{
				Name:  "size",
				Value: 15.0,
				Usage: "примерный размер видео в МБ для авторасчёта воркеров",
			},
			&cli.DurationFlag{
				Name:  "delay",
				Value: 500 * time.Millisecond,
				Usage: "минимальная задержка между задачами одного воркера",
			},
		},
		Action: runDownloader,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}
