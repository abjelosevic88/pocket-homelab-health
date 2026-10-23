package store

import "context"

const importCols = `id, user_id, kind, file, status, progress, samples, workouts, sleep, skipped, error, started_at, finished_at`

func scanImport(row interface{ Scan(...any) error }) (Import, error) {
	var x Import
	if err := row.Scan(&x.ID, &x.UserID, &x.Kind, &x.File, &x.Status, &x.Progress, &x.Samples, &x.Workouts, &x.Sleep, &x.Skipped, &x.Error, &x.StartedAt, &x.FinishedAt); err != nil {
		return x, notFound(err)
	}
	return x, nil
}

// CreateImport starts a job record.
func (s *Store) CreateImport(ctx context.Context, uid int64, kind, file string) (*Import, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO imports (user_id, kind, file) VALUES (?, ?, ?)`, uid, kind, file)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetImport(ctx, uid, id)
}

// UpdateImport saves progress / the outcome.
func (s *Store) UpdateImport(ctx context.Context, x *Import) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE imports SET status = ?, progress = ?, samples = ?, workouts = ?, sleep = ?, skipped = ?, error = ?, finished_at = ? WHERE id = ?`,
		x.Status, x.Progress, x.Samples, x.Workouts, x.Sleep, x.Skipped, x.Error, x.FinishedAt, x.ID)
	return err
}

// GetImport returns one of the person's jobs.
func (s *Store) GetImport(ctx context.Context, uid, id int64) (*Import, error) {
	x, err := scanImport(s.DB.QueryRowContext(ctx, `SELECT `+importCols+` FROM imports WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// ListImports lists the person's last jobs.
func (s *Store) ListImports(ctx context.Context, uid int64) ([]Import, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+importCols+` FROM imports WHERE user_id = ? ORDER BY id DESC LIMIT 20`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Import{}
	for rows.Next() {
		x, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RunningImports marks jobs left "running" by a restart as failed.
func (s *Store) FailStaleImports(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE imports SET status = 'error', error = 'the server restarted during the import', finished_at = ? WHERE status = 'running'`, Now())
	return err
}
