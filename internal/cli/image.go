package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/image"
	"github.com/apomonosi/sandboxing/internal/provider"
)

func newImageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Short: "Manage sandbox images",
	}
	cmd.AddCommand(newImagePullCmd(), newImageBuildCmd())
	return cmd
}

func newImagePullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <ref>",
		Short: "Pull a pre-baked image from a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureImagePull), forcePartial(cmd)); err != nil {
				return err
			}
			return p.ImagePull(cmd.Context(), args[0])
		},
	}
}

func newImageBuildCmd() *cobra.Command {
	var baseImage string
	var packages []string

	cmd := &cobra.Command{
		Use:   "build <output-name>",
		Short: "Build an image just-in-time from a base image plus a package list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseImage == "" {
				return fmt.Errorf("--base is required")
			}
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureImageBuild), forcePartial(cmd)); err != nil {
				return err
			}
			cloudInit, err := image.RenderCloudInit(image.BuildSpec{BaseImage: baseImage, Packages: packages})
			if err != nil {
				return err
			}
			ref, err := p.ImageBuild(cmd.Context(), provider.ImageBuildSpec{
				BaseImage:  baseImage,
				CloudInit:  cloudInit,
				OutputName: args[0],
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), ref)
			return nil
		},
	}
	cmd.Flags().StringVar(&baseImage, "base", "", "base image reference to build from")
	cmd.Flags().StringSliceVar(&packages, "package", nil, "package to install via cloud-init (repeatable)")
	return cmd
}
