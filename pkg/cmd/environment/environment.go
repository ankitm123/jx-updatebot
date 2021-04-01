package environment

import (
	"fmt"
	"os"

	"github.com/jenkins-x-plugins/jx-promote/pkg/environments"
	"github.com/jenkins-x/go-scm/scm"
	v1 "github.com/jenkins-x/jx-api/v4/pkg/apis/jenkins.io/v1"
	"github.com/jenkins-x/jx-helpers/v3/pkg/cmdrunner"
	"github.com/jenkins-x/jx-helpers/v3/pkg/cobras/helper"
	"github.com/jenkins-x/jx-helpers/v3/pkg/kube/jxclient"
	"github.com/jenkins-x/jx-helpers/v3/pkg/kube/jxenv"
	"github.com/jenkins-x/jx-helpers/v3/pkg/options"
	"github.com/jenkins-x/jx-helpers/v3/pkg/termcolor"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// Options the command line options
type Options struct {
	Env              string
	Strategy         string
	PullRequestTitle string
	PullRequestBody  string
	AutoMerge        bool
	GitSetup         bool
	environments.EnvironmentPullRequestOptions
}

var (
	info = termcolor.ColorInfo
)

// NewCmdUpgradeEnvironment creates a command object
func NewCmdUpgradeEnvironment() (*cobra.Command, *Options) {
	o := &Options{}

	cmd := &cobra.Command{
		Use:     "environment",
		Aliases: []string{"env"},
		Short:   "Creates a Pull Request to upgrade the environment git repository from the version stream",
		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			helper.CheckErr(err)
		},
	}

	cmd.Flags().StringVarP(&o.Env, "env", "e", "dev", "the name of the environment to upgrade")
	cmd.Flags().StringVarP(&o.Strategy, "strategy", "s", "", "the 'kpt' strategy to use. To see available strategies type 'kpt pkg update --help'. Typical values are: resource-merge, fast-forward, alpha-git-patch, force-delete-replace")

	cmd.Flags().StringVar(&o.PullRequestTitle, "pull-request-title", "chore: upgrade the cluster git repository from the version stream", "the PR title")
	cmd.Flags().StringVar(&o.PullRequestBody, "pull-request-body", "", "the PR body")
	cmd.Flags().BoolVarP(&o.AutoMerge, "auto-merge", "", false, "should we automatically merge if the PR pipeline is green")
	cmd.Flags().BoolVarP(&o.GitSetup, "git-setup", "", false, "should we setup git first so that we can create Pull Requests")

	o.EnvironmentPullRequestOptions.ScmClientFactory.AddFlags(cmd)

	eo := &o.EnvironmentPullRequestOptions
	cmd.Flags().StringVarP(&eo.CommitTitle, "commit-title", "", "", "the commit title")
	cmd.Flags().StringVarP(&eo.CommitMessage, "commit-message", "", "", "the commit message")
	return cmd, o
}

// Run implements the command
func (o *Options) Run() error {
	err := o.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate options")
	}

	if o.GitSetup {
		err = o.gitSetup()
		if err != nil {
			return errors.Wrapf(err, "failed to setup git")
		}
	}
	ns := o.EnvironmentPullRequestOptions.Namespace
	envMap, envNames, err := jxenv.GetEnvironments(o.EnvironmentPullRequestOptions.JXClient, ns)
	if err != nil {
		return errors.Wrapf(err, "failed to load Environments from namespace %s", ns)
	}
	env := envMap[o.Env]
	if env == nil {
		return options.InvalidOption("env", o.Env, envNames)
	}

	gitURL := env.Spec.Source.URL
	if gitURL == "" {
		return errors.Errorf("the Environment %s has no spec.source.url value so we cannot create a Pull Request", o.Env)
	}

	err = o.upgradeRepository(env, gitURL)
	if err != nil {
		return errors.Wrapf(err, "failed to create Pull Request on repository %s", gitURL)
	}
	return nil
}

func (o *Options) Validate() error {
	var err error

	o.EnvironmentPullRequestOptions.JXClient, o.EnvironmentPullRequestOptions.Namespace, err = jxclient.LazyCreateJXClientAndNamespace(o.EnvironmentPullRequestOptions.JXClient, o.EnvironmentPullRequestOptions.Namespace)
	if err != nil {
		return errors.Wrapf(err, "failed to create jx client")
	}

	// lazy create the git client
	o.EnvironmentPullRequestOptions.Git()
	return nil
}

func (o *Options) upgradeRepository(env *v1.Environment, gitURL string) error {
	// lets clear the branch name so we create a new one each time in a loop
	o.BranchName = ""

	if o.PullRequestTitle == "" {
		o.PullRequestTitle = fmt.Sprintf("chore: upgrade pipelines")
	}
	if o.CommitTitle == "" {
		o.CommitTitle = o.PullRequestTitle
	}
	source := ""
	details := &scm.PullRequest{
		Source: source,
		Title:  o.PullRequestTitle,
		Body:   o.PullRequestBody,
		Draft:  false,
	}

	for _, label := range o.Labels {
		details.Labels = append(details.Labels, &scm.Label{
			Name:        label,
			Description: label,
		})
	}

	o.Function = func() error {
		dir := o.OutDir
		return o.gitopsUpgrade(dir)
	}

	_, err := o.EnvironmentPullRequestOptions.Create(gitURL, "", details, o.AutoMerge)
	if err != nil {
		return errors.Wrapf(err, "failed to create Pull Request on repository %s", gitURL)
	}
	return nil
}

func (o *Options) gitopsUpgrade(dir string) error {
	args := []string{"gitops", "upgrade", "--ignore-yaml-error"}
	if o.Strategy != "" {
		args = append(args, "--strategy", o.Strategy)
	}
	c := &cmdrunner.Command{
		Dir:  dir,
		Name: "jx",
		Args: args,
		Out:  os.Stdout,
		Err:  os.Stdin,
	}
	_, err := o.CommandRunner(c)
	if err != nil {
		return errors.Wrapf(err, "failed to run command %s", c.CLI())
	}
	return nil
}

func (o *Options) gitSetup() error {
	args := []string{"gitops", "git", "setup"}
	c := &cmdrunner.Command{
		Name: "jx",
		Args: args,
		Out:  os.Stdout,
		Err:  os.Stdin,
	}
	_, err := o.CommandRunner(c)
	if err != nil {
		return errors.Wrapf(err, "failed to run command %s", c.CLI())
	}
	return nil
}