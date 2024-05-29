# aws-memory-metrics

Report memory-metrics to AWS CloudWatch

## Install dependencies

```console
$ go get
```

## Build the binary

```console
$ time { go fmt ./main.go && go build -o aws-memory-metrics ./main.go ; }

real	0m0.143s
user	0m0.306s
sys	0m0.142s
```

## Usage

```console
$ ./aws-memory-metrics --help
Usage of ./aws-memory-metrics:
  -p value
    	comma-separated list, to append extra Nextjs=<pid> (shorthand)
  -pids value
    	comma-separated list, to append extra NodeServer=<pid>,Nextjs=<pid>

# can run with zero or more extra pids to report memory usage; deploy time to run with pgrep to get pid of a target application

$ ./aws-memory-metrics -pids Nextjs=$(pgrep --full bin/next --oldest)
```

## deploy the binary to any AWS hosts

with `scp` or `rsync`, copy the binary `aws-memory-metrics` to any AWS hosts and install into crontab

```yaml
command: "crontab -l | grep -q 'aws-memory-metrics' || crontab -l | { cat; echo '* * * * * /path/to/aws-memory-metrics -pids Nextjs=$(pgrep --full bin/next --oldest) &>/dev/null'; } | crontab -"
```
