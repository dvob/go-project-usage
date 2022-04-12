# go-project-usage
go-project-usage lists Github repositories which use a certain Go project. The repository list is sorted by stars so that you can spot more relevant repositories easily. For example if you are intressted which projects are using Nats you can run the following command:
```
$ go-project-usage github.com/nats-io/nats.go
STARS FORKS PROJECT
...
7297  444   https://github.com/tidwall/tile38
8973  885   https://github.com/nats-io/nats-server
9795  844   https://github.com/micro/micro
9876  4159  https://github.com/influxdata/telegraf
15529 1664  https://github.com/asim/go-micro
19621 2025  https://github.com/go-kit/kit
26475 2870  https://github.com/minio/minio
```

To run `go-project-usage` you have to configure a [personal access token](https://docs.github.com/en/github/authenticating-to-github/creating-a-personal-access-token) to access the Github GrapQL API.
```
export GITHUB_TOKEN=...
```
When you create the access token you don't have to select any scopes. The only purpose of the token is that Github can authenticate your requests to the GraphQL API.

## How does it work?
To find the projects and obtain the start count the follwing steps are preformed:
1. Call https://api.godoc.org/importers/<PROJECT> to figure out which projects import the project we are looking for.
2. Call the Github GraphQL API https://api.github.com/graphql to obtain the star and fork count

We use the GraphQL API that we can obtain the star count for many projects with a single API call. If we would use the REST API we would hit the rate limit of Github quite fast.

## Github
### Links
* Rate limit: https://docs.github.com/en/rest/overview/resources-in-the-rest-api#rate-limiting
* GraphQL API Explorer: https://docs.github.com/en/graphql/overview/explorer

### Test Queries with curl
* Configure personal access token for Github
```
export GITHUB_TOKEN=...
```
* Prepare query in  file (e.g. `query.graphql`)
```
{
  _1: repository(name: "pcert", owner: "dvob") {nameWithOwner forkCount isFork isArchived isInOrganization stargazerCount}
  _2: repository(name: "vu", owner: "dvob") {nameWithOwner forkCount isFork isArchived isInOrganization stargazerCount}
}
```

* Run the query
```
curl -v -H "Authorization: Bearer $GITHUB_TOKEN" -d "$( echo '{}' | jq --arg query "$(<query.graphql)" '. + {query: $query}' )" https://api.github.com/graphql
```
