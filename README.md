# dhammapada

## About

Posts entries from The Dhammapada to X at:

[portablebuddha](https://x.com/portablebuddha)

## Pre-reqs

At X, register a Developer App

https://developer.x.com/en/portal/dashboard

Generate:

1. API Key and Secret
2. Access Token and Secret

## Steps

1. Set the following environment variables:
```
# Do you want to just do a dry run? Then set this to 1.
DRY_RUN=1

# X API SECRETS
X_CONSUMER_KEY
X_CONSUMER_SECRET
X_ACCESS_TOKEN
X_ACCESS_SECRET
```

2. Build using the `Makefile`: `make build`
