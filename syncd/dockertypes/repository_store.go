package dockertypes

import (
	digest "github.com/opencontainers/go-digest"
)

type RepositoryStore struct {
	Repositories map[string]respository
}

type respository map[string]digest.Digest

func (repo RepositoryStore) GetAllRepos() map[digest.Digest]struct{} {
	res := map[digest.Digest]struct{}{}
	for _, content := range repo.Repositories {
		for _, ImageID := range content {
			res[ImageID] = struct{}{}
		}
	}
	return res
}
