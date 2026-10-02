package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var evidenceFlags struct {
	Output string
}

var evidenceCmd = &cobra.Command{
	Use:   "evidence <id>",
	Short: "Download a piece of verification evidence, such as a screenshot or trace",
	Long: "Without -o, print a signed URL for the evidence file; it expires after a\n" +
		"few minutes. With -o, download the file to that path, or to stdout with\n" +
		"-o -. Like curl -o, an existing file at the path is overwritten, but only\n" +
		"once the download has started. `aviator scenarios` lists evidence ids.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		evidenceID, err := strconv.Atoi(args[0])
		if err != nil {
			return errors.Errorf("invalid evidence id %q", args[0])
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}

		if evidenceFlags.Output == "" {
			signedURL, err := client.EvidenceURL(cmd.Context(), evidenceID)
			if err != nil {
				return err
			}
			fmt.Println(signedURL)
			return nil
		}

		body, err := client.OpenEvidence(cmd.Context(), evidenceID)
		if err != nil {
			return err
		}
		defer func() { _ = body.Close() }()
		if evidenceFlags.Output == "-" {
			_, err := io.Copy(os.Stdout, body)
			return errors.Wrap(err, "failed to write evidence")
		}

		f, err := os.Create(evidenceFlags.Output)
		if err != nil {
			return errors.Wrapf(err, "failed to create %s", evidenceFlags.Output)
		}
		if _, err := io.Copy(f, body); err != nil {
			_ = f.Close()
			return errors.Wrapf(err, "failed to write %s", evidenceFlags.Output)
		}
		if err := f.Close(); err != nil {
			return errors.Wrapf(err, "failed to write %s", evidenceFlags.Output)
		}
		fmt.Printf("%s Saved evidence %d to %s\n", colors.Success("✓"), evidenceID, evidenceFlags.Output)
		return nil
	},
}

func init() {
	evidenceCmd.Flags().StringVarP(&evidenceFlags.Output, "output", "o", "", "download to this path, overwriting it like curl -o (- for stdout)")
}
