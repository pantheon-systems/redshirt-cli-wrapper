// Copyright © 2017 Pantheon Systems, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	linereader "github.com/mitchellh/go-linereader"
	"github.com/pantheon-systems/go-certauth/certutils"
	"github.com/pantheon-systems/riker/pkg/botpb"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	addr        string
	debug       bool
	certFile    string
	caFile      string
	namespace   string
	description string
	usage       string
	command     string
	users       []string
	groups      []string
)

var client botpb.RikerClient

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "redshirt-cli-wrapper [command]",
	Short: "An wrapper for converting any app into a Redshirt bot",
	Long: `A wrapper for converting any app into a Redshirt bot using a simple
protocol based on STDIN, STDOUT, STDERR.

Example:

	redshirt-cli-wrapper \
		-addr riker:6000 \
		-cert echo.pem \
		-namespace "echo" \
		-description="echo server" \
		--groups "infra" \
		-usage "echo <msg>: replies with <msg>" \
		/bin/echo-server
`,

	// we expect at least one positional arg - the command to execute (with optional args)
	Args:    cobra.MinimumNArgs(1),
	PreRunE: validateArgs,
	RunE:    wrapCmd,
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().StringVarP(
		&addr,
		"addr",
		"a",
		"riker:6000",
		"(required) Address of Riker gRPC server")

	RootCmd.PersistentFlags().BoolVar(
		&debug,
		"debug",
		false,
		"if true turns on debug mode, which does not use TLS")

	RootCmd.PersistentFlags().StringVarP(
		&certFile,
		"cert",
		"c",
		"",
		"(required) Path to TLS client key + certificate (.pem)")

	RootCmd.PersistentFlags().StringVarP(
		&caFile,
		"ca",
		"C",
		"",
		"(required) Path to CA cert for validating the Riker server connection")

	RootCmd.PersistentFlags().StringVarP(
		&namespace,
		"namespace",
		"n",
		"",
		"(required) Command namespace to register with Riker")

	RootCmd.PersistentFlags().StringVarP(
		&description,
		"description",
		"d",
		"",
		"(required) Description of the commands provided by this redshirt")

	RootCmd.PersistentFlags().StringVarP(
		&usage,
		"usage",
		"u",
		"",
		"(required) Usage information for commands provided by this redshirt")

	RootCmd.PersistentFlags().StringSliceVarP(
		&users,
		"users",
		"U",
		[]string{},
		"(required) List of chat usernames authorized to access this redshirt",
	)

	RootCmd.PersistentFlags().StringSliceVarP(
		&groups,
		"groups",
		"G",
		[]string{},
		"(required) List of chat usernames authorized to access this redshirt",
	)
}

func validateArgs(cmd *cobra.Command, args []string) error {
	if addr == "" {
		return errors.New("missing --addr")
	}
	if !debug && certFile == "" {
		return errors.New("missing --cert")
	}
	if description == "" {
		return errors.New("missing --description")
	}
	if usage == "" {
		return errors.New("missing --usage")
	}
	if len(users) == 0 && len(groups) == 0 {
		return errors.New("must specify either --users or --groups")
	}

	return nil
}

// initConfig reads ENV variables if set.
func initConfig() {
	viper.SetEnvPrefix("REDSHIRT_")
	// TODO: i don't think this is working
	viper.AutomaticEnv()
}

func getConnection(keepAliveTime time.Duration, keepAliveTimeout time.Duration, backoffMaxDelay time.Duration) (*grpc.ClientConn, error) {
	authOption := grpc.WithInsecure()
	if !debug {
		// TODO: re-implement cert reloading after we merge our final design into go-certauth/certutils package
		cert, err := certutils.LoadKeyCertFiles(certFile, certFile)
		if err != nil {
			log.Fatalf("Could not load TLS cert '%s': %s", certFile, err.Error())
		}
		// TODO: re-implement cert reloading after we merge our final design into go-certauth/certutils package
		// cm, err := certutils.NewCertReloader(certFile, certFile)
		// if err != nil {
		// 	log.Fatalf("Could not load TLS cert '%s': %s", certFile, err.Error())
		// }

		caPool, err := certutils.LoadCACertFile(caFile)
		if err != nil {
			log.Fatalf("Could not load CA cert '%s': %s", caFile, err.Error())
		}
		tlsConfig := certutils.NewTLSConfig(certutils.TLSConfigModern)
		tlsConfig.RootCAs = caPool
		tlsConfig.Certificates = []tls.Certificate{cert}
		authOption = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	}

	log.Println("Trying to connect to riker at ", addr)
	return grpc.Dial(addr,
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    keepAliveTime,
			Timeout: keepAliveTimeout,
		}),
		authOption,
		grpc.WithBackoffMaxDelay(backoffMaxDelay),
		grpc.WithBlock(), // Blocking on connect is ok here
	)
}

func wrapCmd(cmd *cobra.Command, args []string) error {
	conn, err := getConnection(30*time.Second, 5*time.Second, 10*time.Second)

	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Panic(err)
		}
	}()

	client = botpb.NewRikerClient(conn)
	cap := &botpb.Capability{
		Name:               namespace,
		Usage:              usage,
		Description:        description,
		ForcedRegistration: true,
		Auth: &botpb.CommandAuth{
			Users:  users,
			Groups: groups,
		},
	}

	stream := registerClient(cap)
	for {
		if stream == nil {
			stream = registerClient(cap)
			if stream == nil {
				time.Sleep(3 * time.Second)
			}
			continue
		}

		msg, err := stream.Recv()
		if err == io.EOF {
			continue
		}
		if err != nil {
			log.Printf("error reading message from riker: %+v = %v", client, err)
			stream = nil
			continue
			// TODO
		}

		log.Printf("Got message from riker: %+v\n", msg)
		cmdArgs := segmentMessage(namespace, msg.Payload)

		// ensure we take the command to run + cli args and then add any args passed
		// from chat
		fullArgs := append(args, cmdArgs...)

		reply := &botpb.Message{
			Channel:   msg.Channel,
			Timestamp: msg.Timestamp,
			ThreadTs:  msg.Timestamp,
		}

		envs := os.Environ()
		envs = append(envs, fmt.Sprintf("RIKER_GROUP=%s", strings.Join(msg.Groups, ",")))
		envs = append(envs, fmt.Sprintf("RIKER_NICKNAME=%s", msg.Nickname))

		c := exec.Cmd{
			Path: args[0],
			Args: fullArgs,
			Env:  envs,
		}

		go runCmd(reply, c)
	}
}

func runCmd(reply *botpb.Message, c exec.Cmd) {
	stdout, _ := c.StdoutPipe()
	stderr, _ := c.StderrPipe()

	stderrCopy := &bytes.Buffer{}
	tee := io.TeeReader(stderr, stderrCopy)
	combined := io.MultiReader(stdout, tee)

	err := c.Start()
	if err != nil {
		log.Println("Failed to start command: ", err.Error())
		reply.Payload = "Failed to start command: ```" + err.Error() + "```"
		sendMsg(reply)
		return
	}

	// buffer & flush algorithm:
	// buffer up the line-oriented output from the command as a slice of strings, then
	// send all lines in the buffer whenever 10 lines of output is accumulated, or 2 seconds of time passes.
	// TODO: should we make this configurable? eg: time-flush=2s, lines-flush=10
	lines := []string{}
	lr := linereader.New(combined)
	for {
		brk := false
		flush := false

		select {
		case line, ok := <-lr.Ch:
			if !ok {
				brk = true
			}
			if line != "" {
				lines = append(lines, line)
			}
		case <-time.After(2 * time.Second):
			flush = true
		}

		if len(lines) > 10 {
			flush = true
		}
		if flush && len(lines) > 0 {
			reply.Payload = "```" + strings.Join(lines, "\n") + "```"
			sendMsg(reply)
			lines = lines[:0]
		}
		if brk {
			break
		}
	}

	if err = c.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			reply.Payload = "Command exit with status code > 0:\n ```" + strings.Join(lines, "\n") + "```"
			sendMsg(reply)
			reply.ThreadTs = ""
			reply.Timestamp = ""
			sendMsg(reply)
		}
	}
}

func registerClient(cap *botpb.Capability) botpb.Riker_CommandStreamClient {
	reg, err := client.NewRedShirt(context.Background(), cap)
	if err != nil {
		log.Println("Failed creating the redshirt: ", err.Error())
		return nil
	}

	if reg.CapabilityApplied {
		log.Printf("Rejoice we are the first instance to register namespace '%s'.", namespace)
	} else {
		log.Printf("Starting up as another namespace '%s' minion.", namespace)
	}

	stream, err := client.CommandStream(context.Background(), reg)
	if err != nil {
		log.Println("Error talking to riker: ", err)
	}

	return stream
}

func sendMsg(msg *botpb.Message) {
	resp, err := client.Send(context.Background(), msg)
	if err != nil {
		log.Println("Error sending message to riker: ", err)
	}
	log.Println("Sent!!! ", resp)
}

// segmentMessage  will ensure the string is normalized and broken into its words
func segmentMessage(ns, msg string) []string {
	fields := strings.Fields(msg)

	// check if the field 0th element is addressed to the bot
	if strings.HasPrefix(fields[0], "<@") {
		fields = fields[1:]
	}

	// if the message has teh namespace(cmd) from chat we strip we don't want to pass that
	// to the cli we are invoking
	if fields[0] == ns {
		fields = fields[1:]
	}

	return fields
}
