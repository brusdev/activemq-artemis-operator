package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/google/shlex"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"github.com/pterm/pterm"
)

type RunningCommand struct {
	cmd           *exec.Cmd
	outb          bytes.Buffer
	errb          bytes.Buffer
	cmdPrettyName string
	cancelFunc    context.CancelFunc
	stdout        string
	stderr        string
}

type ExecutableChunk struct {
	Stage          string   `json:"stage"`
	Id             string   `json:"id,omitempty"`
	Requires       string   `json:"requires,omitempty"`
	RootDir        string   `json:"rootdir,omitempty"`
	Runtime        string   `json:"runtime,omitempty"`
	HereTag        string   `json:"hereTag,omitempty"`
	Variables      []string `json:"variables,omitempty"`
	Env            []string `json:"env,omitempty"`
	Result         []string `json:"result,omitempty"`
	ReturnCode     int      `json:"return_code,omitempty"` // result of the last command of the chunk
	IsParallel     bool     `json:"parallel,omitempty"`    //TODO
	Label          string   `json:"label,omitempty"`
	HasBreakpoint  bool     `json:"breakpoint,omitempty"`
	trimedCommands []string
	rawCommands    []string
	commands       []*RunningCommand
}

var dryRun bool = false
var interactive bool = false
var verbose bool = false
var minutesToTimeout int = 10
var startFrom string = ""
var operator_root = "./"
var ingoreBreakpoints bool = false
var updateTutorials bool = false
var tutorials_root = "./docs/tutorials"

func initChunk(params string) (*ExecutableChunk, error) {
	var chunk ExecutableChunk
	err := json.Unmarshal([]byte(params), &chunk)
	chunk.trimedCommands = []string{}
	chunk.ReturnCode = -1
	if len(chunk.Variables) > 0 {
		if chunk.Runtime != "bash" {
			return nil, errors.New("variable extraction are requiring \"runtime\":\"bash\"")
		}
	}
	if chunk.HasBreakpoint {
		pterm.Warning.Println("breakpoint in the document")
	}
	return &chunk, err
}

func Fail(message string, callerSkip ...int) {
	panic(message)
}

func extractStages(tutorial string) [][]*ExecutableChunk {
	var stages [][]*ExecutableChunk
	var err error
	var file *os.File
	filepath := path.Join(tutorials_root, tutorial)
	file, err = os.Open(filepath)
	Expect(err).To(BeNil())
	defer file.Close()
	scanner := bufio.NewScanner(file)
	isInChunk := false
	searchForEof := false
	var currentStage string = ""
	var currentChunk *ExecutableChunk
	var lineCounter = 0
	for scanner.Scan() {
		lineCounter += 1
		if strings.HasPrefix(scanner.Text(), "```{") {
			isInChunk = true
			params, found := strings.CutPrefix(scanner.Text(), "```")
			if found {
				currentChunk, err = initChunk(params)
				if err != nil {
					pterm.Fatal.Printf(fmt.Sprintf("%s@%d %s in %s\n", filepath, lineCounter, err, params))
				}
				if currentStage != currentChunk.Stage {
					stages = append(stages, []*ExecutableChunk{})
					currentStage = currentChunk.Stage
				}
				stages[len(stages)-1] = append(stages[len(stages)-1], currentChunk)
			}
		} else if strings.HasPrefix(scanner.Text(), "```") {
			isInChunk = false
			searchForEof = false
		} else if isInChunk {
			command, isCommand := strings.CutPrefix(scanner.Text(), "$")
			// The line is a command, it starts with a dollar sign
			if isCommand {
				currentChunk.rawCommands = append(currentChunk.rawCommands, scanner.Text())
				currentChunk.trimedCommands = append(currentChunk.trimedCommands, strings.TrimSpace(command))
				// if the chunk is marked as containing a Here Tag, keep appending lines up until the Here Tag is found.
				// What is declaring the beginning of the search of that Here Tag is the fact that it is also present on
				// the command.
				if currentChunk.HereTag != "" && strings.Contains(command, currentChunk.HereTag) {
					searchForEof = true
				}
			} else if searchForEof {
				currentChunk.rawCommands = append(currentChunk.rawCommands, scanner.Text())
				currentChunk.trimedCommands = append(currentChunk.trimedCommands, scanner.Text())
				if strings.HasPrefix(scanner.Text(), currentChunk.HereTag) {
					searchForEof = false
				}
			} else {
				// the line belongs to a sample output of the chunk
				continue
			}
		} else {
			// the line belongs to the markdown section of the document
			continue
		}
	}
	return stages
}

func updateTutorial(tutorial string, stages [][]*ExecutableChunk) error {
	inFPath := path.Join(tutorials_root, tutorial)
	inFile, err := os.Open(inFPath)
	if err != nil {
		return err
	}
	defer inFile.Close()
	outFPath := path.Join(tutorials_root, tutorial+".out")
	outFile, err := os.Create(outFPath)
	if err != nil {
		return err
	}
	defer outFile.Close()
	writer := bufio.NewWriter(outFile)
	scanner := bufio.NewScanner(inFile)
	var isInChunk bool = false
	var writeChunkContent bool = false
	var currentStage int = 0
	var currentChunk int = 0
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "```{") {
			writeChunkContent = true
			// find where we are in the chunks and stages
			if currentChunk == len(stages[currentStage]) {
				currentChunk = 0
				currentStage += 1
			}
		} else if isInChunk && strings.HasPrefix(scanner.Text(), "```") {
			isInChunk = false
			currentChunk += 1
		}
		if !isInChunk {
			_, err = writer.WriteString(scanner.Text() + "\n")
			if err != nil {
				return err
			}
		}
		if writeChunkContent {
			writeChunkContent = false
			isInChunk = true
			// write the content of the chunk from the list of chunks
			stage := stages[currentStage]
			chunk := stage[currentChunk]
			// bash runtimes only have one script to execute
			if chunk.Runtime == "bash" {
				for _, rawCommand := range chunk.rawCommands {
					_, err = writer.WriteString(rawCommand + "\n")
					if err != nil {
						return err
					}
				}
				if chunk.commands[0].stdout != "" {
					writer.WriteString("\n")
					_, err = writer.WriteString(chunk.commands[0].stdout)
					if err != nil {
						return err
					}
				}
			} else {
				for commandIndex, rawCommand := range chunk.rawCommands {
					writer.WriteString(rawCommand + "\n")
					if chunk.commands[commandIndex].stdout != "" {
						writer.WriteString("\n")
						_, err = writer.WriteString(chunk.commands[commandIndex].stdout)
						if err != nil {
							return err
						}
						if commandIndex < len(chunk.rawCommands)-1 {
							writer.WriteString("\n")
						}
					}
				}
			}
		}
	}
	return writer.Flush()
}

func findChunkById(stages [][]*ExecutableChunk, stageName string, chunkId string) *ExecutableChunk {
	for _, stage := range stages {
		if stage[0].Stage != stageName {
			continue
		}
		for _, chunk := range stage {
			if chunk.Id == chunkId {
				return chunk
			}
		}
	}
	return nil
}

func (chunk *ExecutableChunk) write_bash_script(basedir string, script_name string) error {
	scriptPath := path.Join(basedir, script_name)
	f, err := os.Create(scriptPath)
	defer f.Close()
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(f)
	_, err = writer.WriteString("#!/bin/bash\n")
	if err != nil {
		return err
	}
	_, err = writer.WriteString("set -euo pipefail\n")
	if err != nil {
		return err
	}
	for _, line := range chunk.trimedCommands {
		_, err = writer.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	if len(chunk.Variables) > 0 {
		for _, variable := range chunk.Variables {
			_, err = writer.WriteString("echo " + variable + "=$" + variable + "\n")
			if err != nil {
				return err
			}
		}
	}
	err = writer.Flush()
	if err != nil {
		return err
	}
	return os.Chmod(scriptPath, 0770)
}

func (chunk *ExecutableChunk) createCommand(trimedCommand string, tmpDirs map[string]string, variables map[string]string) error {
	var command RunningCommand
	var terminatingError error
	splited, err := shlex.Split(trimedCommand)
	if err != nil {
		return err
	}
	executable := splited[0]
	args := splited[1:]
	ctx := context.Background()
	ctx, command.cancelFunc = context.WithTimeout(context.Background(), time.Duration(minutesToTimeout)*time.Minute)
	command.cmd = exec.CommandContext(ctx, executable, args...)
	// When the chunk requires a specific directory to be executed in
	if chunk.RootDir == "$operator" {
		command.cmd.Dir = operator_root
	} else if strings.HasPrefix(chunk.RootDir, "$tmpdir") {
		// tmpdirs are reusable between commands.
		// so they are stored in a map.
		dirselector := strings.Split(chunk.RootDir, string(os.PathSeparator))[0]
		tmpdir, exists := tmpDirs[dirselector]
		if !exists {
			tmpdir, terminatingError = os.MkdirTemp("/tmp", "*")
			if terminatingError != nil {
				pterm.Error.PrintOnError(terminatingError)
				return terminatingError
			}
			tmpDirs[dirselector] = tmpdir
		}
		command.cmd.Dir = strings.Replace(chunk.RootDir, dirselector, tmpdir, 1)
	} else {
		var tmpdir string
		tmpdir, terminatingError = os.MkdirTemp("/tmp", "*")
		// store the directory for deletion at the end of the process
		tmpDirs[tmpdir] = tmpdir
		if terminatingError != nil {
			pterm.Error.PrintOnError(terminatingError)
			return terminatingError
		}
		command.cmd.Dir = tmpdir
	}
	if chunk.Env != nil {
		for _, v := range chunk.Env {
			command.cmd.Env = append(command.cmd.Env, v+"="+variables[v])
		}
	}
	if chunk.Runtime == "bash" {
		terminatingError = chunk.write_bash_script(command.cmd.Dir, trimedCommand)
		if terminatingError != nil {
			pterm.Error.PrintOnError(terminatingError)
			return terminatingError
		}
	}
	chunk.commands = append(chunk.commands, &command)
	return nil
}

func (command *RunningCommand) initCommandLabel(chunkLabel string) {
	command.cmdPrettyName = strings.Join(command.cmd.Args, " ")
	if chunkLabel != "" {
		command.cmdPrettyName = chunkLabel
	}
	if verbose {
		command.cmdPrettyName = fmt.Sprint(command.cmdPrettyName, " in ", command.cmd.Dir)
		if len(command.cmd.Env) > 0 {
			command.cmdPrettyName = fmt.Sprint(command.cmdPrettyName, " with env ", command.cmd.Env)
		}
	}
}

func (command *RunningCommand) start(chunkLabel string) error {
	command.initCommandLabel(chunkLabel)
	if interactive {
		result, _ := pterm.DefaultInteractiveContinue.WithDefaultText(command.cmdPrettyName).Show()
		if result == "all" {
			interactive = false
		}
		if result == "no" {
			command.cmd = nil
			return nil
		}
		if result == "cancel" {
			return errors.New("User aborted")
		}
	}

	command.cmd.Stdout = &command.outb
	command.cmd.Stderr = &command.errb
	if dryRun {
		return nil
	}
	err := command.cmd.Start()
	if err != nil {
		pterm.Error.Printf("%s: %s\n", command.cmdPrettyName, err)
	}
	return err
}

func (command *RunningCommand) wait(variables map[string]string, chunk *ExecutableChunk) error {
	if !dryRun {
		defer command.cancelFunc()
	}
	if dryRun {
		chunk.ReturnCode = 0
		if len(chunk.Variables) > 0 {
			for _, variable := range chunk.Variables {
				variables[variable] = "dryrun"
			}
		}
		return nil
	}
	var spiner *pterm.SpinnerPrinter
	if interactive {
		spiner, _ = pterm.DefaultSpinner.Start("executing")
	} else {
		spiner, _ = pterm.DefaultSpinner.Start(command.cmdPrettyName)
	}
	if command.cmd == nil {
		spiner.InfoPrinter = &pterm.PrefixPrinter{
			MessageStyle: &pterm.Style{pterm.FgLightBlue},
			Prefix: pterm.Prefix{
				Style: &pterm.Style{pterm.FgBlack, pterm.BgLightBlue},
				Text:  " SKIPPED ",
			},
		}
		spiner.Warning(command.cmdPrettyName)
		return nil
	}
	terminatingError := command.cmd.Wait()
	chunk.ReturnCode = command.cmd.ProcessState.ExitCode()
	if terminatingError != nil {
		spiner.Fail("stdout:\n", command.outb.String(), "\nstderr:\n", command.errb.String(), "\nexit code:", command.cmd.ProcessState.ExitCode())
		return terminatingError
	}
	spiner.Success(command.cmdPrettyName)
	command.stdout = command.outb.String()
	command.stderr = command.errb.String()
	// When there are variables to extract, remove the added echoes from the output
	if len(chunk.Variables) > 0 {
		lines := strings.Split(command.stdout, "\n")
		var newLines []string
		for _, line := range lines {
			isVariableExport := false
			for _, variable := range chunk.Variables {
				content, found := strings.CutPrefix(line, variable+"=")
				if found {
					variables[variable] = content
					isVariableExport = true
				}
			}
			if !isVariableExport {
				newLines = append(newLines, line)
			}
		}
		command.stdout = strings.Join(newLines, "\n")
	}
	if verbose {
		if command.stdout != "" {
			pterm.Info.Println(command.stdout)
		}
		if command.stderr != "" {
			pterm.Warning.Println(command.stderr)
		}
		if len(chunk.Variables) > 0 {
			for _, variable := range chunk.Variables {
				pterm.Info.Println("Extracted variable: $" + variable + "=\"" + variables[variable] + "\"")
			}
		}
	}
	return nil
}

func (command *RunningCommand) executeCommand(variables map[string]string, chunk *ExecutableChunk) error {
	terminatingError := command.start(chunk.Label)
	if terminatingError != nil {
		return terminatingError
	}
	terminatingError = command.wait(variables, chunk)
	return terminatingError
}

func runTutorial(tutorial string) error {
	var tmpDirs map[string]string = make(map[string]string)
	/* chunks can request that their output gets stored to a variable. Only the last command of a chunk will make it to
	 * the variable content
	 */
	var variables map[string]string = make(map[string]string)
	/* when an error occurs during the execution process, every subsequential commands, chunk, and stages are
	 * ignored. As the exception of ones from the `teardown` stage. This is to ensure cleaning up the environment
	 */
	var terminatingError error
	stages := extractStages(tutorial)

	if len(stages) == 0 {
		return nil
	}
	pterm.DefaultSection.Println("Testing " + tutorial)
	for _, chunks := range stages {
		if startFrom != "" {
			if chunks[0].Stage != startFrom {
				continue
			} else {
				startFrom = ""
			}
		}
		if verbose {
			pterm.DefaultSection.WithLevel(2).Printf("stage %s with %d chunks\n", chunks[0].Stage, len(chunks))
		}
		/* About parallel chunks:
		*  - The commands of a parallel chunk are executed sequentially
		*  - The last command of the chunk runs in parallel with the other chunks of the same stage
		*  - They are awaited at the end of the stage where their effect is applied (if they have any, like
		*    setting a variable for instance)
		 */
		var towait []*ExecutableChunk
		for _, chunk := range chunks {
			// In case an error occurred while executing a previous chunk ignore all but teardown chunks
			if terminatingError != nil && chunk.Stage != "teardown" {
				continue
			}
			if chunk.HasBreakpoint && !ingoreBreakpoints {
				interactive = true
			}

			// If a chunk has a requirement on another chunk being executed successfully, find it and check its status
			if chunk.Requires != "" {
				reqStageName := strings.Split(chunk.Requires, "/")[0]
				reqChunkId := strings.Split(chunk.Requires, "/")[1]
				reqChunk := findChunkById(stages, reqStageName, reqChunkId)
				if reqChunk != nil {
					// skip the chunk if its requirement didn't execute well
					if reqChunk.ReturnCode != 0 {
						continue
					}
				}
			}

			// When the runtime is bash, all the commands of the chunk are written in a single script
			if chunk.Runtime == "bash" {
				uuid := uuid.New()
				err := chunk.createCommand("./script"+uuid.String()+".sh", tmpDirs, variables)
				if err != nil {
					terminatingError = err
					continue
				}
				if chunk.IsParallel {
					err = chunk.commands[0].start(chunk.Label)
					if err != nil {
						terminatingError = err
						continue
					}
					towait = append(towait, chunk)
				} else {
					err = chunk.commands[0].executeCommand(variables, chunk)
					if err != nil {
						terminatingError = err
						continue
					}
				}
			} else {
				// Otherwise each command of the chunk gets executed sequentially
				// first create the commands
				for _, trimedCommand := range chunk.trimedCommands {
					err := chunk.createCommand(trimedCommand, tmpDirs, variables)
					if err != nil {
						terminatingError = err
						break
					}
				}
				if terminatingError != nil {
					break
				}
				for commandIndex, command := range chunk.commands {
					// only the last command of a parallel chunk gets executed in parallel of other chunks
					if chunk.IsParallel && commandIndex == len(chunk.trimedCommands)-1 {
						err := command.start(chunk.Label)
						if err != nil {
							terminatingError = err
							break
						}
						towait = append(towait, chunk)
					} else {
						err := command.executeCommand(variables, chunk)
						if err != nil {
							terminatingError = err
							break
						}
					}
				}
			}
		}

		// Wait for the parallel chunks to terminate their execution, if there's a problem running on of the parallel
		// commands, kill all the other ones.
		for _, chunk := range towait {
			lastCommand := chunk.commands[len(chunk.commands)-1]
			if terminatingError != nil {
				pterm.Warning.Printf("Killing %s\n", lastCommand.cmdPrettyName)
				lastCommand.cmd.Process.Kill()
			} else {
				err := lastCommand.wait(variables, chunk)
				if err != nil {
					// In case a parallel chunk got an error executing, we still need to wait for the other chunks to wrap
					// up their work.
					terminatingError = err
				}
			}
		}
	}
	if updateTutorials && terminatingError == nil {
		terminatingError = updateTutorial(tutorial, stages)
		if terminatingError == nil {
			os.Rename(path.Join(tutorials_root, tutorial+".out"), path.Join(tutorials_root, tutorial))
		}
	}
	for _, tmpDir := range tmpDirs {
		os.RemoveAll(tmpDir)
	}
	return terminatingError
}

func main() {
	RegisterFailHandler(Fail)

	var runOnly string
	flag.BoolVar(&dryRun, "dry-run", false, "just list what would be executed without doing it")
	flag.BoolVar(&interactive, "interactive", false, "prompt to press enter between each chunk")
	flag.BoolVar(&verbose, "verbose", false, "print more logs")
	flag.IntVar(&minutesToTimeout, "timeout", 10, "the timeout in minutes for every executed command")
	flag.StringVar(&startFrom, "start-from", "", "start from a specific stage name")
	flag.StringVar(&runOnly, "run-only", "", "Run only a specific file")
	flag.BoolVar(&ingoreBreakpoints, "ignore-breakpoints", false, "ignore the breakpoints")
	flag.BoolVar(&updateTutorials, "update-tutorials", false, "update the chunk output section in the tutorials")
	flag.Parse()
	tutorials, err := os.ReadDir(tutorials_root)
	Expect(err).To(BeNil())
	for _, e := range tutorials {
		if runOnly != "" {
			if path.Base(runOnly) != e.Name() {
				pterm.Info.Println("Ignoring", e.Name())
				continue
			}
		}
		err := runTutorial(e.Name())
		if err != nil {
			panic(err)
		}
	}
}
