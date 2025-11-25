// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

const (
	runCmdUsage = "run INTEGRATION"
	runCmdShort = "start a specific integration by name"
	runCmdLong  = `Start a specific integration by name.
	Every integration can expose a webhook or start a polling mechanism to receive
	data events and have its own configuration options, please refer to the
	documentation for more details.

	The available integrations are:
	- gcp: Google Cloud Platform integration`

	runCmdExample = `# Run the Google Cloud Platform integration
	ibdm run gcp`
	// runLoggerName = "ibdm:run"
)

// RunCmd return the "run" cli command for starting an integration.
func RunCmd() *cobra.Command {
	flags := &runFlags{}
	cmd := &cobra.Command{
		Use:     runCmdUsage,
		Short:   heredoc.Doc(runCmdShort),
		Long:    heredoc.Doc(runCmdLong),
		Example: heredoc.Doc(runCmdExample),

		SilenceErrors: true,
		SilenceUsage:  true,

		ValidArgsFunction: validArgsFunc,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := flags.toOptions(args)
			if err != nil {
				return handleError(cmd, err)
			}

			if err := opts.validate(); err != nil {
				return handleError(cmd, err)
			}

			if err := runIntegration(cmd.Context(), opts); err != nil {
				return handleError(cmd, err)
			}

			return nil
		},
	}

	flags.addFlags(cmd)
	return cmd
}

// runIntegration starts the specified integration.
func runIntegration(ctx context.Context, opts *runOptions) error {
	mappings, err := loadMappingConfigs(opts.mappingPaths)
	if err != nil {
		return err
	}

	var mappers map[string]mapper.Mapper
	if mappers, err = typedMappers(mappings); err != nil {
		return err
	}

	return startIntegration(ctx, opts, mappers, 0)
}

// validArgsFunc provides shell completion for the "run" command.
func validArgsFunc(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	var comps []string
	if len(args) == 0 {
		comps = []cobra.Completion{
			cobra.CompletionWithDesc("gcp", "Google Cloud Platform integration"),
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

// loadMappingConfigs loads all mapping configurations from the provided paths.
func loadMappingConfigs(paths []string) ([]*config.MappingConfig, error) {
	mappings := make([]*config.MappingConfig, 0)
	for _, path := range paths {
		fileMappings, err := config.NewMappingConfigsFromPath(path)
		if err != nil {
			return nil, err
		}

		mappings = append(mappings, fileMappings...)
	}

	return mappings, nil
}

// typedMappers creates a mapper.Mapper for each mapping configuration and return a map of them
// using the mapping type as key.
func typedMappers(mappings []*config.MappingConfig) (map[string]mapper.Mapper, error) {
	typedMappers := make(map[string]mapper.Mapper)
	for _, mapping := range mappings {
		mappings := mapping.Mappings
		mapper, err := mapper.New(mappings.Identifier, mappings.Spec)
		if err != nil {
			return nil, err
		}

		typedMappers[mapping.Type] = mapper
	}

	return typedMappers, nil
}

func startIntegration(ctx context.Context, _ *runOptions, mappers map[string]mapper.Mapper, bufferSize int) error {
	dataChan := make(chan pipeline.Data, bufferSize)
	defer close(dataChan)

	syncChan := make(chan struct{})
	pipeline := pipeline.New(dataChan, mappers, nil)
	go func() {
		pipeline.Run(ctx)
		syncChan <- struct{}{}
	}()

	// create source and start it

	<-syncChan // wait for the pipeline to end
	return nil
}
