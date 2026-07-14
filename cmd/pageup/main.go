package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/desarso/pageup/internal/api"
	"github.com/desarso/pageup/internal/client"
	"github.com/desarso/pageup/internal/protocol"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pageup:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return errors.New("missing HTML file")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "upload":
		return runUpload(args[1:])
	case "keys":
		return runKeys(args[1:])
	case "whoami":
		return runWhoAmI(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "public-key":
		return runPublicKey(args[1:])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return nil
	default:
		return runUpload(args)
	}
}

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("endpoint", client.DefaultEndpoint, "pageup server origin")
	name := flags.String("name", defaultDeviceName(), "name for this device")
	force := flags.Bool("force", false, "replace existing credentials")
	jsonOutput := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("init does not accept positional arguments")
	}
	path, err := client.DefaultConfigPath()
	if err != nil {
		return err
	}
	config, err := client.GenerateConfig(*endpoint, *name)
	if err != nil {
		return err
	}
	if err := client.SaveConfig(path, config, *force); err != nil {
		return err
	}
	publicKey, _ := config.PublicKey()
	publicKeyValue := protocol.EncodePublicKey(publicKey)
	if *jsonOutput {
		return printJSON(map[string]string{
			"config":     path,
			"endpoint":   config.Endpoint,
			"key_id":     config.KeyID,
			"public_key": publicKeyValue,
			"name":       config.Name,
		})
	}
	fmt.Printf("Created credentials: %s\n", path)
	fmt.Printf("Key ID: %s\n", config.KeyID)
	fmt.Printf("Public key: %s\n\n", publicKeyValue)
	fmt.Println("Approve this device from an already-authorized computer:")
	fmt.Printf("  pageup keys add --name %q %s\n", config.Name, publicKeyValue)
	return nil
}

func runUpload(args []string) error {
	flags := flag.NewFlagSet("upload", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "print JSON")
	openPage := flags.Bool("open", false, "open the uploaded page")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pageup [--json] [--open] <file.html|->")
	}
	html, err := readHTML(flags.Arg(0))
	if err != nil {
		return err
	}
	pageup, _, err := configuredClient()
	if err != nil {
		return err
	}
	context, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := pageup.Upload(context, html)
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		fmt.Println(result.URL)
	}
	if *openPage {
		return openURL(result.URL)
	}
	return nil
}

func runKeys(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pageup keys <add|list|revoke>")
	}
	switch args[0] {
	case "add":
		flags := flag.NewFlagSet("keys add", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		name := flags.String("name", "", "device name")
		role := flags.String("role", "upload", "upload or admin")
		jsonOutput := flags.Bool("json", false, "print JSON")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || flags.NArg() != 1 {
			return errors.New("usage: pageup keys add --name <device> [--role upload|admin] <public-key>")
		}
		pageup, _, err := configuredClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		key, err := pageup.AddKey(ctx, api.AddKeyRequest{Name: *name, PublicKey: flags.Arg(0), Role: *role})
		if err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(key)
		}
		fmt.Printf("Authorized %s (%s) as %s\n", key.Name, key.ID, key.Role)
		return nil
	case "list":
		flags := flag.NewFlagSet("keys list", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		jsonOutput := flags.Bool("json", false, "print JSON")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: pageup keys list [--json]")
		}
		pageup, _, err := configuredClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		keys, err := pageup.ListKeys(ctx)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(api.KeyListResponse{Keys: keys})
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].CreatedAt.Before(keys[j].CreatedAt) })
		writer := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tNAME\tROLE\tCREATED")
		for _, key := range keys {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", key.ID, key.Name, key.Role, key.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		return writer.Flush()
	case "revoke":
		flags := flag.NewFlagSet("keys revoke", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("usage: pageup keys revoke <key-id>")
		}
		pageup, _, err := configuredClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		key, err := pageup.RevokeKey(ctx, flags.Arg(0))
		if err != nil {
			return err
		}
		fmt.Printf("Revoked %s (%s)\n", key.Name, key.ID)
		return nil
	default:
		return fmt.Errorf("unknown keys command %q", args[0])
	}
}

func runWhoAmI(args []string) error {
	if len(args) != 0 {
		return errors.New("whoami does not accept arguments")
	}
	pageup, _, err := configuredClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key, err := pageup.WhoAmI(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%s, %s)\n", key.Name, key.ID, key.Role)
	return nil
}

func runDoctor(args []string) error {
	if len(args) != 0 {
		return errors.New("doctor does not accept arguments")
	}
	pageup, config, err := configuredClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	health, err := pageup.Health(ctx)
	if err != nil {
		return fmt.Errorf("server health check failed: %w", err)
	}
	key, err := pageup.WhoAmI(ctx)
	if err != nil {
		return fmt.Errorf("credential check failed: %w", err)
	}
	fmt.Printf("endpoint   %s\n", config.Endpoint)
	fmt.Printf("server     %s (%s)\n", health["status"], health["version"])
	fmt.Printf("credential %s (%s, %s)\n", key.Name, key.ID, key.Role)
	return nil
}

func runPublicKey(args []string) error {
	if len(args) != 0 {
		return errors.New("public-key does not accept arguments")
	}
	config, err := client.LoadConfig("")
	if err != nil {
		return err
	}
	publicKey, err := config.PublicKey()
	if err != nil {
		return err
	}
	fmt.Println(protocol.EncodePublicKey(publicKey))
	return nil
}

func configuredClient() (*client.Client, client.Config, error) {
	config, err := client.LoadConfig("")
	if err != nil {
		return nil, client.Config{}, err
	}
	pageup, err := client.New(config, version)
	return pageup, config, err
}

func readHTML(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(io.LimitReader(os.Stdin, (5<<20)+1))
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("HTML path is a directory")
	}
	if info.Size() > 5<<20 {
		return nil, errors.New("HTML file exceeds the 5 MiB upload limit")
	}
	return os.ReadFile(path)
}

func openURL(value string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", value)
	case "windows":
		command = exec.Command("cmd", "/c", "start", "", value)
	default:
		command = exec.Command("xdg-open", value)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func defaultDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "this computer"
	}
	return hostname
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `pageup — private HTML uploads, shareable URLs

Usage:
  pageup <file.html>                   upload in one command
  pageup -                            upload HTML from stdin
  pageup init [--endpoint URL]        create this device's key pair
  pageup keys add --name NAME PUBKEY  authorize another device
  pageup keys list                    list authorized devices
  pageup keys revoke KEY_ID           revoke a device
  pageup whoami                       show the active credential
  pageup doctor                       verify server and authentication

Upload options (place before the file):
  --json  emit a machine-readable result
  --open  open the resulting URL`)
}
