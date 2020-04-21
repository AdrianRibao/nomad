package command

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
)

type LicensePutCommand struct {
	Meta

	testStdin io.Reader
}

func (c *LicensePutCommand) Help() string {
	helpText := `
Usage: nomad license put [options]

Puts a new license in Servers and Clients

General Options:

  ` + generalOptionsUsage() + `

Install a new license from a file:

	$ nomad license put @nomad.license

Install a new license from stdin:

	$ nomad license put -

Install a new license from a string:

	$ nomad license put "<license blob>"
	`
	return helpText
}

func (c *LicensePutCommand) Synopsis() string {
	return "Install a new Nomad Enterprise License"
}

func (c *LicensePutCommand) Name() string { return "license put" }

func (c *LicensePutCommand) Run(args []string) int {
	flags := c.Meta.FlagSet(c.Name(), FlagSetClient)
	flags.Usage = func() { c.Ui.Output(c.Help()) }

	if err := flags.Parse(args); err != nil {
		c.Ui.Error(fmt.Sprintf("Error parsing flags: %s", err))
		return 1
	}

	args = flags.Args()
	data, err := c.dataFromArgs(args)
	if err != nil {
		c.Ui.Error(errors.Wrap(err, "Error parsing arguments").Error())
		return 1
	}

	client, err := c.Meta.Client()
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error initializing client: %s", err))
		return 1
	}

	resp, _, err := client.Operator().LicensePut(data, nil)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error putting license: %v", err))
		return 1
	}

	return OutputLicenseReply(c.Ui, resp)
}

func (c *LicensePutCommand) dataFromArgs(args []string) (string, error) {
	switch len(args) {
	case 0:
		return "", fmt.Errorf("Missing LICENSE argument")
	case 1:
		return LoadDataSource(args[0], c.testStdin)
	default:
		return "", fmt.Errorf("Too many arguments, exptected 1, got %d", len(args))
	}
}

func loadFromFile(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("Failed to read file: %v", err)
	}
	return string(data), nil
}

func loadFromStdin(testStdin io.Reader) (string, error) {
	var stdin io.Reader = os.Stdin
	if testStdin != nil {
		stdin = testStdin
	}

	var b bytes.Buffer
	if _, err := io.Copy(&b, stdin); err != nil {
		return "", fmt.Errorf("Failed to read stdin: %v", err)
	}
	return b.String(), nil
}

func LoadDataSource(data string, testStdin io.Reader) (string, error) {
	// Handle empty quoted shell parameters
	if len(data) == 0 {
		return "", nil
	}

	switch data[0] {
	case '@':
		return loadFromFile(data[1:])
	case '-':
		if len(data) > 1 {
			return data, nil
		}
		return loadFromStdin(testStdin)
	default:
		return data, nil
	}
}
