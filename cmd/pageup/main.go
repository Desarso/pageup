package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/desarso/pageup/internal/api"
	"github.com/desarso/pageup/internal/client"
	"github.com/desarso/pageup/internal/pageskill"
	"github.com/desarso/pageup/internal/protocol"
	"github.com/desarso/pageup/internal/sitebundle"
)

var version = "dev"

// fileTransferTimeout bounds a whole file upload, which can be far larger
// than an HTML page.
const fileTransferTimeout = 30 * time.Minute

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
	case "update":
		return runUpdate(args[1:])
	case "file", "files":
		return runFile(args[1:])
	case "delete":
		return runDelete(args[1:])
	case "keys":
		return runKeys(args[1:])
	case "whoami":
		return runWhoAmI(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "public-key":
		return runPublicKey(args[1:])
	case "skill":
		return runSkill(args[1:])
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
		return errors.New("usage: pageup [--json] [--open] <file.html|site-directory|file|->; use 'pageup file' for several files")
	}
	if hosted, err := isHostedFile(flags.Arg(0)); err != nil {
		return err
	} else if hosted {
		return uploadFiles(flags.Args(), "", *jsonOutput, *openPage)
	}
	artifact, err := readArtifact(flags.Arg(0))
	if err != nil {
		return err
	}
	pageup, _, err := configuredClient()
	if err != nil {
		return err
	}
	context, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var result api.UploadResponse
	if artifact.site {
		result, err = pageup.UploadSite(context, artifact.body)
	} else {
		result, err = pageup.Upload(context, artifact.body)
	}
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

func runUpdate(args []string) error {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "print JSON")
	openPage := flags.Bool("open", false, "open the updated page")
	name := flags.String("name", "", "rename a hosted file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: pageup update [--json] [--open] [--name NAME] <URL-or-UUID> <file.html|site-directory|file|->")
	}
	pageup, config, err := configuredClient()
	if err != nil {
		return err
	}
	id, kind, err := parseTarget(flags.Arg(0), config.Endpoint)
	if err != nil {
		return err
	}
	if kind == targetAny {
		hosted, err := isHostedFile(flags.Arg(1))
		if err != nil {
			return err
		}
		if hosted {
			kind = targetFile
		}
	}
	if kind == targetFile {
		return updateFile(pageup, id, flags.Arg(1), *name, *jsonOutput, *openPage)
	}
	if *name != "" {
		return errors.New("--name applies only to hosted files")
	}
	artifact, err := readArtifact(flags.Arg(1))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var result api.UploadResponse
	if artifact.site {
		result, err = pageup.UpdateSite(ctx, id, artifact.body)
	} else {
		result, err = pageup.Update(ctx, id, artifact.body)
	}
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

func runFile(args []string) error {
	flags := flag.NewFlagSet("file", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "print JSON")
	openFile := flags.Bool("open", false, "open the uploaded files")
	name := flags.String("name", "", "published file name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return errors.New("usage: pageup file [--json] [--open] [--name NAME] <path...|->")
	}
	if *name != "" && flags.NArg() != 1 {
		return errors.New("--name applies to a single file")
	}
	return uploadFiles(flags.Args(), *name, *jsonOutput, *openFile)
}

func uploadFiles(paths []string, name string, jsonOutput, openFiles bool) error {
	pageup, _, err := configuredClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	defer cancel()
	results := make([]api.FileResponse, 0, len(paths))
	for _, path := range paths {
		result, err := uploadFile(ctx, pageup, path, name)
		if err != nil {
			if jsonOutput && len(results) > 0 {
				printJSON(results)
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		if !jsonOutput {
			fmt.Println(result.URL)
		}
		results = append(results, result)
	}
	if jsonOutput {
		var err error
		if len(results) == 1 {
			err = printJSON(results[0])
		} else {
			err = printJSON(results)
		}
		if err != nil {
			return err
		}
	}
	if openFiles {
		for _, result := range results {
			if err := openURL(result.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

func uploadFile(ctx context.Context, pageup *client.Client, path, name string) (api.FileResponse, error) {
	if name == "" {
		if path == "-" {
			return api.FileResponse{}, errors.New("--name is required when sharing standard input")
		}
		name = filepath.Base(path)
	}
	content, closeContent, err := openFileContent(path)
	if err != nil {
		return api.FileResponse{}, err
	}
	defer closeContent()
	return pageup.UploadFile(ctx, name, content)
}

func updateFile(pageup *client.Client, id, path, name string, jsonOutput, openFile bool) error {
	content, closeContent, err := openFileContent(path)
	if err != nil {
		return err
	}
	defer closeContent()
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferTimeout)
	defer cancel()
	result, err := pageup.UpdateFile(ctx, id, name, content)
	if err != nil {
		return err
	}
	if jsonOutput {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		fmt.Println(result.URL)
	}
	if openFile {
		return openURL(result.URL)
	}
	return nil
}

// openFileContent opens a file to share. Standard input is spooled to a
// temporary file because uploads are hashed before they are sent.
func openFileContent(path string) (*os.File, func(), error) {
	if path == "-" {
		temporary, err := os.CreateTemp("", "pageup-stdin-*")
		if err != nil {
			return nil, nil, err
		}
		discard := func() {
			temporary.Close()
			os.Remove(temporary.Name())
		}
		if _, err := io.Copy(temporary, os.Stdin); err != nil {
			discard()
			return nil, nil, err
		}
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			discard()
			return nil, nil, err
		}
		return temporary, discard, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, errors.New("directories cannot be shared as files; archive it first, or publish an HTML site with 'pageup DIRECTORY'")
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("only regular files can be shared")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return file, func() { file.Close() }, nil
}

// isHostedFile reports whether path should be shared as a file rather than
// published as an HTML page. Files without an extension are sniffed so HTML
// written to a temporary file still becomes a page.
func isHostedFile(path string) (bool, error) {
	if path == "-" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".xhtml":
		return false, nil
	case "":
		file, err := os.Open(path)
		if err != nil {
			return false, err
		}
		defer file.Close()
		head := make([]byte, 512)
		count, err := io.ReadFull(file, head)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return false, err
		}
		return !strings.HasPrefix(http.DetectContentType(head[:count]), "text/html"), nil
	default:
		return true, nil
	}
}

func runDelete(args []string) error {
	flags := flag.NewFlagSet("delete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pageup delete [--json] <file-URL-or-UUID>")
	}
	pageup, config, err := configuredClient()
	if err != nil {
		return err
	}
	id, kind, err := parseTarget(flags.Arg(0), config.Endpoint)
	if err != nil {
		return err
	}
	if kind == targetPage {
		return errors.New("only hosted files can be deleted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := pageup.DeleteFile(ctx, id)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(result)
	}
	fmt.Printf("Deleted %s\n", result.URL)
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

func runSkill(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pageup skill <show|install>")
	}
	switch args[0] {
	case "show":
		if len(args) != 1 {
			return errors.New("usage: pageup skill show")
		}
		content, err := pageskill.SkillMarkdown()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(content)
		return err
	case "install":
		flags := flag.NewFlagSet("skill install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		harness := flags.String("harness", "auto", "auto, codex, agents, or project")
		target := flags.String("target", "", "custom skills directory")
		force := flags.Bool("force", false, "replace embedded files in an existing Pages skill")
		jsonOutput := flags.Bool("json", false, "print JSON")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: pageup skill install [--harness auto|codex|agents|project] [--target DIR] [--force]")
		}
		root, resolvedHarness, err := resolveSkillRoot(*harness, *target)
		if err != nil {
			return err
		}
		path, err := pageskill.Install(root, *force)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(map[string]string{
				"skill":   pageskill.Name,
				"harness": resolvedHarness,
				"path":    path,
			})
		}
		fmt.Printf("Installed $%s for %s at %s\n", pageskill.Name, resolvedHarness, path)
		fmt.Println("Start a new agent session to discover the skill.")
		return nil
	default:
		return fmt.Errorf("unknown skill command %q (expected show or install)", args[0])
	}
}

func resolveSkillRoot(harness, target string) (string, string, error) {
	if target != "" {
		if harness != "" && harness != "auto" {
			return "", "", errors.New("use either --target or --harness, not both")
		}
		path, err := absoluteUserPath(target)
		return path, "custom", err
	}
	if value := strings.TrimSpace(os.Getenv("PAGEUP_SKILLS_DIR")); value != "" && (harness == "" || harness == "auto") {
		path, err := absoluteUserPath(value)
		return path, "custom", err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	if harness == "" {
		harness = "auto"
	}
	if harness == "auto" {
		if os.Getenv("CODEX_HOME") != "" {
			harness = "codex"
		} else if info, statErr := os.Stat(filepath.Join(home, ".codex")); statErr == nil && info.IsDir() {
			harness = "codex"
		} else if info, statErr := os.Stat(filepath.Join(home, ".agents")); statErr == nil && info.IsDir() {
			harness = "agents"
		} else {
			harness = "codex"
		}
	}

	var root string
	switch harness {
	case "codex":
		base := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if base == "" {
			base = filepath.Join(home, ".codex")
		}
		root = filepath.Join(base, "skills")
	case "agents":
		root = filepath.Join(home, ".agents", "skills")
	case "project":
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		root = filepath.Join(workingDirectory, ".agents", "skills")
	default:
		return "", "", fmt.Errorf("unsupported harness %q (use auto, codex, agents, project, or --target DIR)", harness)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	return root, harness, nil
}

func absoluteUserPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("skills directory cannot be empty")
	}
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, value[2:])
		}
	}
	return filepath.Abs(value)
}

func configuredClient() (*client.Client, client.Config, error) {
	config, err := client.LoadConfig("")
	if err != nil {
		return nil, client.Config{}, err
	}
	pageup, err := client.New(config, version)
	return pageup, config, err
}

type uploadArtifact struct {
	body []byte
	site bool
}

func readArtifact(path string) (uploadArtifact, error) {
	if path == "-" {
		body, err := io.ReadAll(io.LimitReader(os.Stdin, sitebundle.DefaultMaxBytes+1))
		if err != nil {
			return uploadArtifact{}, err
		}
		if int64(len(body)) > sitebundle.DefaultMaxBytes {
			return uploadArtifact{}, errors.New("HTML file exceeds the 5 MiB upload limit")
		}
		return uploadArtifact{body: body}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return uploadArtifact{}, err
	}
	if info.IsDir() {
		archive, err := sitebundle.Pack(path, sitebundle.DefaultMaxBytes)
		if err != nil {
			return uploadArtifact{}, err
		}
		return uploadArtifact{body: archive, site: true}, nil
	}
	if info.Size() > sitebundle.DefaultMaxBytes {
		return uploadArtifact{}, errors.New("HTML file exceeds the 5 MiB upload limit")
	}
	body, err := os.ReadFile(path)
	return uploadArtifact{body: body}, err
}

type targetKind int

const (
	// targetAny is a bare UUIDv7, which may name either a page or a file.
	targetAny targetKind = iota
	targetPage
	targetFile
)

func parseTarget(value, endpoint string) (string, targetKind, error) {
	value = strings.TrimSpace(value)
	if protocol.IsUUIDv7(value) {
		return value, targetAny, nil
	}
	targetURL, err := url.Parse(value)
	if err != nil || !targetURL.IsAbs() || targetURL.User != nil {
		return "", targetAny, errors.New("target must be a UUIDv7 or a Pageup URL")
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return "", targetAny, errors.New("configured Pageup endpoint is invalid")
	}
	if !strings.EqualFold(targetURL.Scheme, endpointURL.Scheme) || !strings.EqualFold(targetURL.Host, endpointURL.Host) {
		return "", targetAny, fmt.Errorf("Pageup URL must belong to %s", strings.TrimRight(endpoint, "/"))
	}
	if rest, ok := strings.CutPrefix(targetURL.Path, "/f/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		if !protocol.IsUUIDv7(id) {
			return "", targetAny, errors.New("Pageup file URL does not contain a valid UUIDv7 file id")
		}
		return id, targetFile, nil
	}
	id := strings.Trim(targetURL.Path, "/")
	if !protocol.IsUUIDv7(id) {
		return "", targetAny, errors.New("Pageup URL does not contain a valid UUIDv7 page id")
	}
	return id, targetPage, nil
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
  pageup <file.html|site-directory>    upload in one command
  pageup -                            upload HTML from stdin
  pageup <file>                       share any other file (image, PDF, log, archive)
  pageup file <path...|->             share one or more files, one URL per line
  pageup update URL <path|->          replace a page, site, or file at the same URL
  pageup delete FILE_URL              delete a shared file
  pageup init [--endpoint URL]        create this device's key pair
  pageup keys add --name NAME PUBKEY  authorize another device
  pageup keys list                    list authorized devices
  pageup keys revoke KEY_ID           revoke a device
  pageup whoami                       show the active credential
  pageup doctor                       verify server and authentication
  pageup skill show                   print the embedded $pages skill
  pageup skill install                add $pages to this agent harness
  pageup public-key                   print this device's public key
  pageup version                      print the CLI version

Upload options (place before the file):
  --json  emit a machine-readable result
  --open  open the resulting URL

Update options (place before the URL):
  pageup update --json URL file.html  emit revision and update state as JSON
  pageup update --open UUID file.html update by id and open the page

HTML site directories:
  Include index.html at the root and up to 100 .html files total.
  Nested folders are preserved. CSS and JavaScript must remain inline;
  share images and other assets as files and reference their URLs.

File sharing:
  Files are public but unlisted at /f/<id>/<name>. Images, PDFs, audio,
  video, text, and JSON open in the browser; other types download.
  Add ?download to a file URL to force a download.
  pageup file --name out.log -          share stdin under a file name
  pageup update --name v2.pdf URL f.pdf replace a file and rename it

Skill installation:
  pageup skill install                         auto-detect Codex or ~/.agents
  pageup skill install --harness project       install into ./.agents/skills
  pageup skill install --target /skills/root   install for any other harness
  Add --force to update an existing embedded $pages skill.`)
}
