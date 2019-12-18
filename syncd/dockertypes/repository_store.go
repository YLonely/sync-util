package dockertypes

import (
	digest "github.com/opencontainers/go-digest"
)

type FlattenRepos map[digest.Digest]struct{}

type RepositoryStore struct {
	Repositories map[string]repository `json:"Repositories,omitempty"`
}

type repository map[string]digest.Digest

func (repo RepositoryStore) GetAllRepos() FlattenRepos {
	res := map[digest.Digest]struct{}{}
	for _, content := range repo.Repositories {
		for _, ImageID := range content {
			res[ImageID] = struct{}{}
		}
	}
	return res
}
