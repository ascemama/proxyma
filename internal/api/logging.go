package api

import "log"

func (s *Server) debugf(format string, args ...any) {
	if !s.Debug {
		return
	}
	s.logf(format, args...)
}

func (s *Server) logf(format string, args ...any) {
	logger := s.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf(format, args...)
}
