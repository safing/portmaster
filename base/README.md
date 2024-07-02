> **Check out our main project at [safing/portmaster](https://github.com/safing/portmaster)**

# Portbase

Portbase helps you quickly take off with your project. It gives you all the basic needs you would have for a service (_not_ tool!).
Here is what is included:

- `log`: really fast and beautiful logging
- `modules`: a multi stage, dependency aware boot process for your software, also manages tasks
- `config`: simple, live updating and extremely fast configuration storage
- `info`: easily tag your builds with versions, commit hashes, and so on
- `formats`: some handy data encoding libs
- `rng`: a feedable CSPRNG for great randomness
- `database`: intelligent and syncable database with hooks and easy integration with structs, uses buckets with different backends
- `api`: a websocket interface to the database, can be extended with custom http handlers

Before you continue, a word about this project. It was created to hold the base code for both Portmaster and Gate17. This is also what it will be developed for. If you have a great idea on how to improve portbase, please, by all means, raise an issue and tell us about it, but please also don't be surprised or offended if we ask you to create your own fork to do what you need. Portbase isn't for everyone, it's quite specific to our needs, but we decided to make it easily available to others.

Portbase is actively maintained, please raise issues.

## log

The main goal of this logging package is to be as fast as possible. Logs are sent to a channel only with minimal processing beforehand, so that the service can continue with the important work and write the logs later.

Second, is beauty, both in form what information is provided and how.

You can use flags to change the log level on a source file basis.

## modules <small>requires `log`</small>

packages may register themselves as modules, to take part in the multi stage boot and coordinated shutdown.

Registering only requires a name/key and the `prep()`, `start()` and `stop()` functions.

This is how modules are booted:

- `init()` available: ~~flags~~, ~~config~~, ~~logging~~, ~~dependencies~~
  - register flags (with the stdlib `flag` library)
  - register module
- `module.prep()` available: flags, ~~config~~, ~~logging~~, ~~dependencies~~
  - react to flags
  - register config variables
  - if an error occurs, return it
  - return ErrCleanExit for a clean, successful exit. (eg. you only printed a version)
- `module.start()` available: flags, config, logging, dependencies
  - start tasks and workers
  - do not log errors while starting, but return them
- `module.stop()` available: flags, config, logging, dependencies
  - stop all work (ie. goroutines)
  - do not log errors while stopping, but return them

You can start tasks and workers from your module that are then integrated into the module system and will allow for insights and better control of them in the future.

## config <small>requires `log`</small>

The config package stores the configuration in json strings. This may sound a bit weird, but it's very practical.

There are three layers of configuration - in order of priority: user configuration, default configuration and the fallback values supplied when registering a config variable.

When using config variables, you get a function that checks if your config variable is still up to date every time. If it did not change, it's _extremely_ fast. But if it, it will fetch the current value, which takes a short while, but does not happen often.

    // This is how you would get a string config variable function.
    myVar := GetAsString("my_config_var", "default")
    // You then use myVar() directly every time, except when you must guarantee the same value between two calls
    if myVar() != "default" {
      log.Infof("my_config_var is set to %s", myVar())
    }
    // no error handling needed! :)

WARNING: While these config variable functions are _extremely_ fast, they are _NOT_ thread/goroutine safe! (Use the `Concurrent` wrapper for that!)

## info

Info provides a easy way to store your version and build information within the binary. If you use the `build` script to build the program, it will automatically set build information so that you can easily find out when and from which commit a binary was built.

The `build` script extracts information from the host and the git repo and then calls `go build` with some additional arguments.

## formats/varint

This is just a convenience wrapper around `encoding/binary`, because we use varints a lot.

## formats/dsd <small>requires `formats/varint`</small>

DSD stands for dynamically structured data. In short, this a generic packer that reacts to the supplied data type.

- structs are usually json encoded
- []bytes and strings stay the same

This makes it easier / more efficient to store different data types in a k/v data storage.

## rng <small>requires `log`, `config`</small>

This package provides a CSPRNG based on the [Fortuna](https://en.wikipedia.org/wiki/Fortuna_(PRNG)) CSPRNG, devised by Bruce Schneier and Niels Ferguson. Implemented by Jochen Voss, published [on Github](https://github.com/seehuhn/fortuna).

Only the Generator is used from the `fortuna` package. The feeding system implemented here is configurable and is focused with efficiency in mind.

While you can feed the RNG yourself, it has two feeders by default:
- It starts with a seed from `crypto/rand` and periodically reseeds from there
- A really simple tickfeeder which extracts entropy from the internal go scheduler using goroutines and is meant to be used under load.

## database <small>requires `log`</small>
_introduction to be written_

## api <small>requires `log`, `database`, `config`</small>
_introduction to be written_

## The main program

If you build everything with modules, your main program should be similar to this - just use an empty import for the modules you need:

    import (
      "os"
      "os/signal"
      "syscall"

      "github.com/safing/portmaster/base/info"
      "github.com/safing/portmaster/base/log"
      "github.com/safing/portmaster/base/modules"

      // include packages here
      _ "path/to/my/custom/module"
    )

    func main() {

    	// Set Info
    	info.Set("MySoftware", "1.0.0")

    	// Start
    	err := modules.Start()
    	if err != nil {
    		if err == modules.ErrCleanExit {
    			os.Exit(0)
    		} else {
    			os.Exit(1)
    		}
    	}

    	// Shutdown
    	// catch interrupt for clean shutdown
    	signalCh := make(chan os.Signal)
    	signal.Notify(
    		signalCh,
    		os.Interrupt,
    		syscall.SIGHUP,
    		syscall.SIGINT,
    		syscall.SIGTERM,
    		syscall.SIGQUIT,
    	)
    	select {
    	case <-signalCh:
    		log.Warning("main: program was interrupted")
    		modules.Shutdown()
    	case <-modules.ShuttingDown():
    	}

    }
