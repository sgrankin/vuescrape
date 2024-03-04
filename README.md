Export Emporia Vue data into VictoriaMetrics.

- https://www.netatmo.com

## Install

Build from source or `go install sgrankin.dev/vuescrape@latest`.

## Run

`vuescrap -dest=host:part`

The destination host is expected to be VictoriaMetrics:

- prometheus query routes are used to find the timestamp of the last written sample (for incremental updates).
- victoria metrics raw JSON import is used to push data.

Run as a cron job every 10-60 minutes to avoid overwhelming the Vue servers. See [this issue] for discussion.

[this issue]: https://github.com/magico13/PyEmVue/issues/19]

## References

- https://github.com/magico13/PyEmVue/blob/master/api_docs.md
- https://github.com/magico13/PyEmVue/blob/master/pyemvue/auth.py
- https://github.com/jertel/vuegraf
