/*
Copyright 2022 The Matrix.org Foundation C.I.C.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/matrix-org/waterfall/pkg/config"
	"github.com/matrix-org/waterfall/pkg/profiling"
	"github.com/matrix-org/waterfall/pkg/routing"
	"github.com/matrix-org/waterfall/pkg/signaling"
	"github.com/matrix-org/waterfall/pkg/webrtc_ext"
	"github.com/sirupsen/logrus"
	"maunium.net/go/mautrix/event"
)

func main() {
	// Parse command line flags.
	var (
		configFilePath = flag.String("config", "config.yaml", "configuration file path")
		cpuProfile     = flag.String("cpuProfile", "", "write CPU profile to `file`")
		memProfile     = flag.String("memProfile", "", "write memory profile to `file`")
	)
	flag.Parse()

	// Initialize logging subsystem (formatting, global logging framework etc).
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, ForceColors: true})

	// Define functions that are called before exiting.
	// This is useful to stop the profiler if it's enabled.
	deferred_functions := []func(){}
	if *cpuProfile != "" {
		deferred_functions = append(deferred_functions, profiling.InitCPUProfiling(cpuProfile))
	}
	if *memProfile != "" {
		deferred_functions = append(deferred_functions, profiling.InitMemoryProfiling(memProfile))
	}

	// Handle signal interruptions.
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		for _, function := range deferred_functions {
			function()
		}
		os.Exit(0)
	}()

	// Load the config file from the environment variable or path.
	config, err := config.LoadConfig(*configFilePath)
	if err != nil {
		logrus.WithError(err).Fatal("could not load config")
		return
	}

	switch config.LogLevel {
	case "trace":
		logrus.SetLevel(logrus.TraceLevel)
	case "debug":
		logrus.SetLevel(logrus.DebugLevel)
	case "info", "": // default to info level if unset
		logrus.SetLevel(logrus.InfoLevel)
	case "warn":
		logrus.SetLevel(logrus.WarnLevel)
	case "error":
		logrus.SetLevel(logrus.ErrorLevel)
	case "fatal":
		logrus.SetLevel(logrus.FatalLevel)
	case "panic":
		logrus.SetLevel(logrus.PanicLevel)
	default:
		logrus.Fatalf("unrecognised log level: %s", config.LogLevel)
	}

	// Create matrix client.
	matrixClient := signaling.NewMatrixClient(config.Matrix)

	// Create a pre-configured factory for the peer connections.
	connectionFactory, err := webrtc_ext.NewPeerConnectionFactory(config.WebRTC)
	if err != nil {
		logrus.WithError(err).Fatal("could not create peer connection factory")
		return
	}

	// Create a channel which we'll use to send events to the router.
	matrixEvents := make(chan *event.Event)
	defer close(matrixEvents)

	// Start a router that will receive events from the matrix client and route them to the appropriate conference.
	routing.StartRouter(matrixClient, connectionFactory, matrixEvents, config.Conference)

	// Start matrix client sync. This function will block until the sync fails.
	if err := matrixClient.RunSync(func(e *event.Event) { matrixEvents <- e }); err != nil {
		logrus.WithError(err).Fatal("matrix client sync failed")
		return
	}

	logrus.Info("SFU stopped")
}
