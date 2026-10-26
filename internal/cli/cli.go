package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
	"github.com/daviddwlee84/lazycred/internal/service"
	"github.com/daviddwlee84/lazycred/internal/tui"
	"github.com/daviddwlee84/lazycred/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func secret(ctx context.Context, label string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", model.Fail("interactive_required", "This operation requires an interactive terminal; secrets are not accepted in argv.")
	}
	fmt.Fprint(os.Stderr, label)
	type result struct {
		value []byte
		err   error
	}
	state, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return "", model.Fail("interactive_required", "Cannot read terminal state.")
	}
	done := make(chan result, 1)
	go func() { value, err := term.ReadPassword(int(os.Stdin.Fd())); done <- result{value, err} }()
	var data []byte
	select {
	case r := <-done:
		data, err = r.value, r.err
	case <-ctx.Done():
		_ = term.Restore(int(os.Stdin.Fd()), state)
		return "", model.Fail("cancelled", "Secret input cancelled.")
	}
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", model.Fail("cancelled", "Secret input cancelled.")
	}
	return string(data), nil
}
func confirm(ctx context.Context, serial string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return model.Fail("interactive_required", "Confirmation requires an interactive terminal.")
	}
	fmt.Fprintf(os.Stderr, "Target %s. Type this exact serial to apply (anything else cancels): ", serial)
	done := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(os.Stdin).ReadString('\n'); done <- line }()
	var line string
	select {
	case line = <-done:
	case <-ctx.Done():
		return model.Fail("cancelled", "Cancelled before write.")
	}
	if strings.TrimSpace(line) != serial {
		return model.Fail("cancelled", "Cancelled before write.")
	}
	return nil
}
func find(ctx context.Context, app *service.App, source, category, id string) (model.Item, error) {
	rows, err := app.Items(ctx, source, category)
	if err != nil {
		return model.Item{}, err
	}
	var found *model.Item
	for _, row := range rows {
		if row.ID == id {
			if found != nil {
				return model.Item{}, model.Fail("ambiguous", "This ID occurs in multiple sources. Select an exact --source and --category.")
			}
			copy := row
			found = &copy
		}
	}
	if found != nil {
		return *found, nil
	}
	return model.Item{}, model.Fail("not_found", "Exact item ID not found in this source/category.")
}

func Command() *cobra.Command {
	var cfgPath, source, category string
	var demo, readOnly, jsonOutput bool
	var bwPath, uvPath string
	app := func(cmd *cobra.Command) (*service.App, error) {
		c, err := config.Load(cfgPath)
		if err != nil {
			return nil, err
		}
		if cmd.Flags().Changed("bw") || cmd.InheritedFlags().Changed("bw") {
			c.BW = bwPath
		}
		if cmd.Flags().Changed("uv") || cmd.InheritedFlags().Changed("uv") {
			c.UV = uvPath
		}
		if strings.TrimSpace(c.BW) == "" || strings.TrimSpace(c.UV) == "" {
			return nil, model.Fail("invalid", "Executable overrides must not be empty.")
		}
		if err := validateSelection(source, category); err != nil {
			return nil, err
		}
		return service.New(c, demo, readOnly), nil
	}
	output := func(cmd *cobra.Command, value any) error {
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
		}
		switch v := value.(type) {
		case []model.Source:
			for _, s := range v {
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s  %-24s  %s\n", model.Safe(s.ID), model.Safe(s.Name), model.Safe(s.State))
			}
		case []model.Item:
			for _, i := range v {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  [%s]  %s\n", model.Safe(i.ID), model.Safe(i.Name), model.Safe(i.Kind), model.Safe(i.Subtitle))
			}
		case model.Item:
			fmt.Fprintf(cmd.OutOrStdout(), "%s · %s\n", model.Safe(v.Name), model.Safe(v.Source))
			for _, f := range v.Fields {
				value := f.Value
				if f.Secret {
					value = "[redacted]"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", model.Safe(f.Label), model.Safe(value))
			}
		case string:
			fmt.Fprintln(cmd.OutOrStdout(), v)
		default:
			return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
		}
		return nil
	}
	root := &cobra.Command{Use: "lazycred", Short: "A credential workbench over YubiKey Manager and Bitwarden", SilenceUsage: true, SilenceErrors: true, Version: version.Current()}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "Explicit TOML configuration path")
	root.PersistentFlags().StringVar(&source, "source", "overview", "Source ID, e.g. bitwarden or yubikey:SERIAL")
	root.PersistentFlags().StringVar(&category, "category", "status", "YubiKey: status/oath/fido/piv/openpgp; vault: all/login/card/note/ssh")
	root.PersistentFlags().BoolVar(&demo, "demo", false, "Use fictional data only; never touch backends or clipboard")
	root.PersistentFlags().BoolVar(&readOnly, "read-only", false, "Disable setup, management, transfer writes and HOTP counters")
	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Machine-readable metadata (secret fields always redacted)")
	root.PersistentFlags().StringVar(&bwPath, "bw", "", "Override the bw executable")
	root.PersistentFlags().StringVar(&uvPath, "uv", "", "Override the uv executable")
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unexpected argument; see lazycred --help")
		}
		a, err := app(cmd)
		if err != nil {
			return err
		}
		if jsonOutput {
			rows, err := a.Sources(cmd.Context())
			if err != nil {
				return err
			}
			return output(cmd, rows)
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return cmd.Help()
		}
		return tui.Run(cmd.Context(), a, source, category)
	}
	sources := &cobra.Command{Use: "sources", Short: "List sources and local authentication state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		rows, err := a.Sources(cmd.Context())
		if err != nil {
			return err
		}
		return output(cmd, rows)
	}}
	root.AddCommand(sources)
	devices := &cobra.Command{Use: "devices", Short: "YubiKey inventory"}
	devices.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		rows, err := a.Sources(cmd.Context())
		if err != nil {
			return err
		}
		var keys []model.Source
		for _, s := range rows {
			if s.Kind == "yubikey" || s.Kind == "reader" || s.Kind == "setup" {
				keys = append(keys, s)
			}
		}
		return output(cmd, keys)
	}})
	devices.AddCommand(&cobra.Command{Use: "show SERIAL", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		rows, err := a.Items(cmd.Context(), "yubikey:"+args[0], "status")
		if err != nil {
			return err
		}
		return output(cmd, rows)
	}})
	root.AddCommand(devices)
	items := &cobra.Command{Use: "items", Short: "Browse metadata and redacted item details"}
	items.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		rows, err := a.Items(cmd.Context(), source, category)
		if err != nil {
			return err
		}
		return output(cmd, rows)
	}})
	items.AddCommand(&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		item, err := find(cmd.Context(), a, source, category, args[0])
		if err != nil {
			return err
		}
		item, err = a.Detail(cmd.Context(), item)
		if err != nil {
			return err
		}
		return output(cmd, item)
	}})
	root.AddCommand(items)
	var field, hotpConfirm string
	copyCmd := &cobra.Command{Use: "copy ID", Short: "Copy one explicit field; never print its value", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		if field == "" {
			return fmt.Errorf("--field is required; use items show to see field IDs")
		}
		item, err := find(cmd.Context(), a, source, category, args[0])
		if model.Code(err) == "locked" && strings.HasPrefix(source, "yubikey:") && !jsonOutput {
			password, e := secret(cmd.Context(), "Application PIN/password: ")
			if e != nil {
				return e
			}
			if _, e = a.UnlockYubi(cmd.Context(), source, category, password); e != nil {
				return e
			}
			item, err = find(cmd.Context(), a, source, category, args[0])
		}
		if err != nil {
			return err
		}
		message, err := a.Copy(cmd.Context(), item, field, hotpConfirm)
		if err != nil {
			return err
		}
		return output(cmd, map[string]any{"copied": true, "message": message, "clipboard_seconds": a.Config.ClipboardSeconds})
	}}
	copyCmd.Flags().StringVar(&field, "field", "", "Exact field ID")
	copyCmd.Flags().StringVar(&hotpConfirm, "confirm-serial", "", "Explicit consent to advance a HOTP counter")
	root.AddCommand(copyCmd)
	var exportField, exportPath string
	exportCmd := &cobra.Command{Use: "export ID", Short: "Export only public keys/certificates to a new private file", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		if exportField == "" || exportPath == "" {
			return fmt.Errorf("--field and --output are required")
		}
		item, err := find(cmd.Context(), a, source, category, args[0])
		if err != nil {
			return err
		}
		if err = a.Export(cmd.Context(), item, exportField, exportPath); err != nil {
			return err
		}
		return output(cmd, "Public data exported (demo mode simulates this operation).")
	}}
	exportCmd.Flags().StringVar(&exportField, "field", "", "certificate/public_key/fingerprint")
	exportCmd.Flags().StringVar(&exportPath, "output", "", "New output path; existing files are never overwritten")
	root.AddCommand(exportCmd)
	var identifier, name string
	manage := &cobra.Command{Use: "manage ACTION", Short: "Confirmed common management; secrets are prompted, not flags", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		if a.ReadOnly {
			return model.Fail("read_only", "Management disabled.")
		}
		s, ok := strings.CutPrefix(source, "yubikey:")
		if !ok {
			return fmt.Errorf("--source yubikey:SERIAL is required")
		}
		valid := map[string]bool{"oath-add": true, "oath-delete": true, "oath-password": true, "fido-delete": true, "fido-pin": true, "piv-pin": true, "openpgp-pin": true}
		if !valid[args[0]] {
			return fmt.Errorf("unsupported action; use oath-add/oath-delete/oath-password/fido-delete/fido-pin/piv-pin/openpgp-pin")
		}
		if strings.HasSuffix(args[0], "delete") && identifier == "" {
			return fmt.Errorf("--id is required for deletion")
		}
		if args[0] == "oath-add" && name == "" {
			return fmt.Errorf("--name is required for oath-add")
		}
		if jsonOutput {
			return model.Fail("interactive_required", "Management requires interactive secret input; no JSON prompting.")
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Action %s · source %s · exact item %s\n", args[0], source, model.Safe(identifier))
		if err = confirm(cmd.Context(), s); err != nil {
			return err
		}
		current, err := secret(cmd.Context(), "Current application PIN/password (empty only if unset): ")
		if err != nil {
			return err
		}
		next := ""
		if !strings.HasSuffix(args[0], "delete") {
			label := "New PIN/password: "
			if args[0] == "oath-add" {
				label = "TOTP seed or otpauth URI: "
			}
			next, err = secret(cmd.Context(), label)
			if err != nil {
				return err
			}
			if args[0] != "oath-add" {
				repeat, e := secret(cmd.Context(), "Repeat new PIN/password: ")
				if e != nil {
					return e
				}
				if repeat != next {
					return model.Fail("invalid", "New secrets do not match; nothing changed.")
				}
			}
		}
		message, err := a.Write(cmd.Context(), source, args[0], identifier, name, next, current, s)
		if err != nil {
			return err
		}
		return output(cmd, message)
	}}
	manage.Flags().StringVar(&identifier, "id", "", "Exact credential ID, never substring matching")
	manage.Flags().StringVar(&name, "name", "", "New OATH account label")
	root.AddCommand(manage)
	var transferTarget, transferName string
	var preview bool
	transfer := &cobra.Command{Use: "transfer", Short: "Copy selected TOTP; retain the Bitwarden source"}
	transferTOTP := &cobra.Command{Use: "totp ITEM_UUID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		if transferTarget == "" {
			return fmt.Errorf("--to yubikey:SERIAL is required")
		}
		if jsonOutput {
			return model.Fail("interactive_required", "Use the TUI or an interactive terminal for transfer secrets.")
		}
		s := strings.TrimPrefix(transferTarget, "yubikey:")
		if !preview {
			if err = confirm(cmd.Context(), s); err != nil {
				return err
			}
		}
		password, err := secret(cmd.Context(), "Destination OATH password (empty if unset): ")
		if err != nil {
			return err
		}
		message, err := a.Transfer(cmd.Context(), args[0], transferTarget, transferName, password, s, preview)
		if err != nil {
			return err
		}
		return output(cmd, message)
	}}
	transferTOTP.Flags().StringVar(&transferTarget, "to", "", "Exact destination yubikey:SERIAL")
	transferTOTP.Flags().StringVar(&transferName, "name", "", "Optional target account label")
	transferTOTP.Flags().BoolVar(&preview, "dry-run", false, "Validate destination without writing (reads vault and hardware)")
	transfer.AddCommand(transferTOTP)
	root.AddCommand(transfer)
	setup := &cobra.Command{Use: "setup", Short: "Explicitly prepare backend dependencies"}
	setup.AddCommand(&cobra.Command{Use: "yubikey", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Preparing pinned official SDK environment with uv; this may download dependencies.")
		if err = a.Setup(cmd.Context()); err != nil {
			return err
		}
		return output(cmd, "YubiKey helper ready; no device settings changed.")
	}})
	root.AddCommand(setup)
	cfg := &cobra.Command{Use: "config", Short: "Preferences; never contains credentials"}
	cfg.AddCommand(&cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		return output(cmd, a.Config)
	}})
	root.AddCommand(cfg)
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Read-only configuration and backend discovery", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := app(cmd)
		if err != nil {
			return err
		}
		sources, err := a.Sources(cmd.Context())
		if err != nil {
			return err
		}
		data := a.Diagnostic()
		data["sources"] = sources
		return output(cmd, data)
	}})
	root.AddCommand(&cobra.Command{Use: "__clipboard-clear", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var request service.ClearRequest
		if json.NewDecoder(io.LimitReader(os.Stdin, 2<<20)).Decode(&request) != nil {
			return nil
		}
		return service.ClearIfOwned(cmd.Context(), service.NativeClipboard{}, request)
	}})
	return root
}
func Execute(ctx context.Context, args []string) error {
	cmd := Command()
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func validateSelection(source, category string) error {
	if source != "overview" && source != "all" && source != "bitwarden" {
		value, ok := strings.CutPrefix(source, "yubikey:")
		if !ok || value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return model.Fail("invalid", "Use --source overview/all/bitwarden/yubikey:SERIAL.")
		}
	}
	valid := map[string]bool{"status": true, "all": true, "oath": true, "fido": true, "piv": true, "openpgp": true, "login": true, "card": true, "note": true, "identity": true, "ssh": true}
	if !valid[category] {
		return model.Fail("invalid", "Unknown category; see --help.")
	}
	if source == "bitwarden" && (category == "oath" || category == "fido" || category == "piv" || category == "openpgp") {
		return model.Fail("invalid", "This category belongs to a YubiKey source.")
	}
	if strings.HasPrefix(source, "yubikey:") && (category == "login" || category == "card" || category == "note" || category == "identity" || category == "ssh") {
		return model.Fail("invalid", "This category belongs to the Bitwarden source.")
	}
	return nil
}
