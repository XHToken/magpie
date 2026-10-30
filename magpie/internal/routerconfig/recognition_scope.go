package routerconfig

import "context"

// ConfirmRecognitionScopeContext linearizes attempt admission with application
// revocation. begin must only update in-memory admission state, never do I/O or
// call back into this Store while its configuration lock is held.
func (s *Store) ConfirmRecognitionScopeContext(ctx context.Context, id, expected string, begin func() error) error {
	ctx, unlock, err := s.lockContext(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	files, err := s.providerFilesContext(ctx)
	if err != nil {
		return err
	}
	apps, err := readApps(files.document)
	if err != nil {
		return err
	}
	app, ok := apps[id]
	if !ok {
		return ErrAppNotFound
	}
	if app.RecognitionScope() != expected || app.Cloud.ProviderID == "" && len(app.Models) == 0 {
		return ErrAppConflict
	}
	return begin()
}
