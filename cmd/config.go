package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/5Sigma/pmx/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage pmx configuration",
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path to the config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.ConfigPath()
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := config.Load()
		if errors.Is(err, config.ErrNoConfigFile) {
			fmt.Println("No config file found (defaults will be used)")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Println("OK")
		return nil
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the config file in $EDITOR",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.ConfigPath()
		if err != nil {
			return err
		}

		// Ensure the file exists before opening.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			cfg := config.Default()
			if err := config.SaveTo(cfg, path); err != nil {
				return fmt.Errorf("creating default config: %w", err)
			}
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vi"
		}

		// Support editor commands with arguments (e.g. "code --wait").
		parts := strings.Fields(editor)
		c := exec.Command(parts[0], append(parts[1:], path)...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("editor exited with error: %w", err)
		}

		// Validate after edit.
		if _, err := config.Load(); err != nil && !errors.Is(err, config.ErrNoConfigFile) {
			fmt.Fprintf(os.Stderr, "warning: config may have errors: %v\n", err)
		}
		return nil
	},
}

func init() {
	configCmd.AddCommand(configPathCmd, configValidateCmd, configEditCmd)
	rootCmd.AddCommand(configCmd)
}
