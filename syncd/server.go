package syncd

//Config contains all configurable properties
type Config struct {
	SuperNodeIP, SuperNodePort string
	ImageMetaDataDir           string
	ImageLayerDataDir          string
	SyncDir                    string
}

//Server represents a syncd server
type Server struct {
	superNodeIP, superNodePort string
	clsPort                    string
	metaDataDir, layerDataDir  string
	syncDir                    string
}

//NewServer init a server instance
func NewServer(c Config) (*Server, error) {
	s := &Server{
		superNodeIP:   c.SuperNodeIP,
		superNodePort: c.SuperNodePort,
		metaDataDir:   c.ImageMetaDataDir,
		layerDataDir:  c.ImageLayerDataDir,
		syncDir:       c.SyncDir,
	}
	return s, nil
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() error {
	return nil
}
