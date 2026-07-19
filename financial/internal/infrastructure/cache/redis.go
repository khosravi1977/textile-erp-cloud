package cache

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	addr     string
	password string
	db       int
	timeout  time.Duration
}

func NewFromEnv() *Client {
	host := envOr("REDIS_HOST", "localhost")
	port := envOr("REDIS_PORT", "6379")
	db, _ := strconv.Atoi(envOr("REDIS_DB", "0"))
	return &Client{
		addr:     net.JoinHostPort(host, port),
		password: os.Getenv("REDIS_PASSWORD"),
		db:       db,
		timeout:  time.Duration(envInt("REDIS_TIMEOUT_MS", 500)) * time.Millisecond,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && !strings.EqualFold(os.Getenv("CACHE_DISABLED"), "true")
}

func TenantKey(companyID int64, parts ...string) string {
	if companyID <= 0 {
		companyID = 1
	}
	keyParts := append([]string{"tenant", strconv.FormatInt(companyID, 10)}, parts...)
	return strings.Join(keyParts, ":")
}

func (c *Client) Get(ctx context.Context, key string) ([]byte, bool, error) {
	reply, err := c.command(ctx, "GET", key)
	if err != nil {
		if errors.Is(err, errNil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return []byte(reply), true, nil
}

func (c *Client) SetJSON(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		_, err := c.command(ctx, "SET", key, string(value))
		return err
	}
	_, err := c.command(ctx, "SET", key, string(value), "EX", strconv.Itoa(int(ttl.Seconds())))
	return err
}

func (c *Client) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	args := append([]string{"DEL"}, keys...)
	_, err := c.command(ctx, args...)
	return err
}

func (c *Client) command(ctx context.Context, args ...string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("cache disabled")
	}
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	conn, err := net.DialTimeout("tcp", c.addr, time.Until(deadline))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	reader := bufio.NewReader(conn)
	if c.password != "" {
		if err := writeCommand(conn, "AUTH", c.password); err != nil {
			return "", err
		}
		if _, err := readReply(reader); err != nil {
			return "", err
		}
	}
	if c.db > 0 {
		if err := writeCommand(conn, "SELECT", strconv.Itoa(c.db)); err != nil {
			return "", err
		}
		if _, err := readReply(reader); err != nil {
			return "", err
		}
	}
	if err := writeCommand(conn, args...); err != nil {
		return "", err
	}
	return readReply(reader)
}

func writeCommand(conn net.Conn, args ...string) error {
	if _, err := fmt.Fprintf(conn, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

var errNil = errors.New("redis nil")

func readReply(r *bufio.Reader) (string, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return "", errors.New(line)
	case ':':
		return line, nil
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return "", err
		}
		if n < 0 {
			return "", errNil
		}
		buf := make([]byte, n+2)
		if _, err := r.Read(buf); err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	case '*':
		return line, nil
	default:
		return "", fmt.Errorf("unsupported redis reply: %q", prefix)
	}
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(envOr(key, strconv.Itoa(fallback)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
