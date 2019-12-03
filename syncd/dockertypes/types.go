package dockertypes

type RepositoryStore struct {
	Repositories map[string]respository
}

type respository map[string]string
