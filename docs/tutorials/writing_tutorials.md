---
title: "Writing executable tutorials"
description: "How to write an executable tutorial"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 110
toc: true
---

### The terminology: Stages, chunks and commands

Executable tutorials work a bit like a CI workflow. The command is the smallest
element of them. It corresponds to an executable with its arguments to be called
onto the system. A chunk is a collection of commands and is used to group them
with an environment of execution, chunks can be executed in parallel of each
other within the same stage. And finally, there are the stages, which are a
collection of chunks. Stages gets executed one after the other.

#### Code chunks

In an executable tutorial, all is ignored but the code chunks. Chunks that are
executed are chunks that provide a valid json object with at least a stage
configured in it.

The code fend to create an executable chunk needs to have 3 back quotes
immediately followed by an opening curly bracket.

````
```{"stage":"init"}
```
````

The only mandatory field an executable chunk must have is the stage name.

The other possible fields are:

##### `"runtime":"bash"`

Specify that all the commands in the chunk are bash commands. The chunk will get
turned into a script.

The script itself is prefixed with `set -euo pipefail`, meaning that it will
error at the first failing commands.

##### `"HereTag":"EOF"`

In conjunction with a bash runtime, you can specify that the content contains a
Here Tag. All the lines until a line starting by EOF will be taken into the
script.

This chunk must only have a single command in it.

##### `"label":"some label"`

You can give a pretty name to a chunk. It's good for bash runtimes, as the
command to execute is always `./script.sh` so if you want to have a
differentiable name in the logs, use this field.

##### `"variables":["NAME"]`

Extracts the given variables from the environment of the chunk. This works only
in conjunction with a bash environment. User can use this to either extract
content they are creating or system variables, such as EDITOR for instance.

##### `"env":["NAME=value"]`

Gives environment variables to all the commands of the chunk.

##### `"rootdir":"$operator"`

The commands in the chunk will the executed from the operator source code
directory.


##### `"rootdir":"$tmpdir.x"`

Create a new temporary directory to execute the chunk. If another chunk reuses
the same suffix (`.x` in the example) it'll share the same directory. Useful
when you need to chain chunks or to reuse some cached files between chunks

##### `"parallel":"true"`

Makes the chunk executed in parallel of the others in its stage. This works best
if there's only one command in the chunk (or if the chunk is a bash chunk). And
if all the commands in the stage are made to run in parallel.

For now the parallel behavior has a simplistic implementation. Only the last
command of the chunk is really started asynchronously. The other ones are
sequentially executed before that.

##### `"breakpoint":"true"`

Enters interactive mode when the chunk is started. Useful for debugging
purposes. Better to use alongside `--verbose`

##### `"requires":"stageName/id"`

Makes the execution of the given stage dependant of the correct execution of the
pointed stage. To be used in teardown chunks.

##### `"id":"someID"`

Give an ID to a chunk so that it can get referenced later on

#### Commands

Commands start with a `$` in front of them

### Examples

````
```{"stage":"test1"}
$ echo a line starting with dollar gets executed, and its output is shown under

a line starting with dollar gets executed, and its output is shown under
```
````

Running the `go run test/utils/tutorials/tester.go` command will execute
the above chunk and produce:

````
```
╼> go run test/utils/tutorials/tester.go --run-only writing_tutorials.md

# Testing writing_tutorials.md

 SUCCESS  echo a line starting with dollar gets executed, and its output is shown under
```
````

(Note that this chunk isn't executed as there's no JSON metadata associated)

Let's have a verbose view:

```
╼> go run test/utils/tutorials/tester.go --verbose --run-only writing_tutorials.md
# Testing writing_tutorials.md


## stage init with 1 chunks


## stage test1 with 1 chunks

 SUCCESS  echo a line starting with dollar gets executed, and its output is shown under in /tmp/3361630480
 INFO  a line starting with dollar gets executed, and its output is shown under

```

We can see that the `init` chunk we've created at the beginning of the document
is also taken into account, even though there's no command to execute it that
chunk.

Let's add a bit of complexity, we will create two chunks, the first one extract
several variables and the second one consumes one of them.

````
```{"stage":"test2", "runtime":"bash", "variables":["VARIABLE", "TEST", "EDITOR"]}
$ VARIABLE=$(sleep .1s && echo "This is some content")
$ TEST="some value"
```
````

````
```{"stage":"test2", "env":["VARIABLE"], "runtime":"bash"}
$ if [ "$VARIABLE" == "This is some content" ]; then
$   echo "same string"
$ fi

same string
```
````

This is getting us:

```
## stage test2 with 2 chunks

 SUCCESS  ./scriptc0ea8682-5725-40b1-b3af-895b10538147.sh in /tmp/2216367646
 INFO  Extracted variable: $VARIABLE="This is some content"
 INFO  Extracted variable: $TEST="some value"
 INFO  Extracted variable: $EDITOR="nvim"
 SUCCESS  ./scriptf6c0cf71-e244-4301-a51a-3d471bc44b13.sh in /tmp/1769345675 with env [VARIABLE=This is some content]
 INFO  out: same string
```

Let's add some parallelism. A stage will perform changes to a common
file with two concurrent processes. Then a last stage will collect the changes
and display them.

````
```{"stage":"test3", "runtime":"bash", "parallel":true, "rootdir":"$tmpdir.1"}
$ for i in $(seq 1 10);
$ do
$     echo FIRST$i >> output
$     sleep .1
$ done
```
````

````
```{"stage":"test3", "runtime":"bash", "parallel":true, "rootdir":"$tmpdir.1"}
$ for i in $(seq 1 10);
$ do
$     sleep .1
$     echo SECOND$i >> output
$ done
```
````

````
```{"stage":"test4", "rootdir":"$tmpdir.1"}
$ cat output

FIRST1
SECOND1
FIRST2
SECOND2
FIRST3
FIRST4
SECOND3
FIRST5
SECOND4
FIRST6
SECOND5
FIRST7
SECOND6
SECOND7
FIRST8
SECOND8
FIRST9
FIRST10
SECOND9
SECOND10
```
````

This gives us the result:

```
## stage test3 with 2 chunks

 SUCCESS  ./scriptbdfcb2a4-495a-4a1b-9ccc-bf0545d6e58a.sh in /tmp/577314846
 SUCCESS  ./script0e605abe-f19c-4fe8-ac9d-448d836f2945.sh in /tmp/577314846

## stage test4 with 2 chunks

 SUCCESS  cat output in /tmp/577314846
 INFO  FIRST1
       SECOND1
       FIRST2
       SECOND2
       FIRST3
       FIRST4
       SECOND3
       FIRST5
       SECOND4
       FIRST6
       SECOND5
       FIRST7
       SECOND6
       SECOND7
       FIRST8
       SECOND8
       FIRST9
       FIRST10
       SECOND9
       SECOND10
```

Using HereTags:

````
```{"stage":"test4", "runtime":"bash", "HereTag":"EOF"}
$ cat << EOF
something
printed using a HereTag EOF
EOF
$ sleep 1
$ cat << EOF
some other thing
printed using a HereTag EOF
too
EOF

something
printed using a HereTag EOF
some other thing
printed using a HereTag EOF
too
```
````

This gives us the result:

```
 SUCCESS  ./script1c0e4c1d-a0b8-4708-b1ca-a21513f84ff3.sh in /tmp/1419374313
 INFO  something
       printed
       some other thing
       printed
       too
```
