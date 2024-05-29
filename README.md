# aws-memory-metrics
report memory-metrics to AWS CloudWatch

## build

```console
$ time { go fmt ./main.go && go build -o aws-memory-metrics ./main.go ; }

real	0m0.143s
user	0m0.306s
sys	0m0.142s
```

## deploy the binary to any AWS hosts

with `scp` or `rsync`, copy the binary `aws-memory-metrics` to any AWS hosts and install into crontab

```yaml
    command: "crontab -l | grep -q 'aws-memory-metrics' || crontab -l | { cat; echo '* * * * * /path/to/aws-memory-metrics &>/dev/null'; } | crontab -"
```
