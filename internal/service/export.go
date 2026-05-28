package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"
)

type ExportService struct {
	pool        *pgxpool.Pool
	diagnostics *DiagnosticsService
	conflicts   *ConflictsService
}

func NewExportService(pool *pgxpool.Pool, diag *DiagnosticsService, conf *ConflictsService) *ExportService {
	return &ExportService{pool: pool, diagnostics: diag, conflicts: conf}
}

type ExportKind string

const (
	ExportUpcomingVacations ExportKind = "upcoming_vacations"
	ExportStaleProfiles     ExportKind = "stale_profiles"
	ExportConflicts         ExportKind = "conflicts"
	ExportAllEmployees      ExportKind = "all_employees"
)

type ExportResult struct {
	Filename string
	Data     []byte
}

var ErrUnknownExportKind = errors.New("export: unknown kind")

func (s *ExportService) Build(ctx context.Context, kind ExportKind) (*ExportResult, error) {
	switch kind {
	case ExportUpcomingVacations:
		return s.buildUpcomingVacations(ctx)
	case ExportStaleProfiles:
		return s.buildStaleProfiles(ctx)
	case ExportConflicts:
		return s.buildConflicts(ctx)
	case ExportAllEmployees:
		return s.buildAllEmployees(ctx)
	default:
		return nil, ErrUnknownExportKind
	}
}

type ExportDataset struct {
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Headers []string `json:"headers"`
	Rows    [][]any  `json:"rows"`
}

type DatasetOptions struct {
	From           *time.Time
	To             *time.Time
	Departments    []string
	Columns        []string
	Kinds          []string
	RestrictEmpIDs []uuid.UUID
}

func (opts *DatasetOptions) restrictedSet() map[string]struct{} {
	if len(opts.RestrictEmpIDs) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(opts.RestrictEmpIDs))
	for _, id := range opts.RestrictEmpIDs {
		out[id.String()] = struct{}{}
	}
	return out
}

func (s *ExportService) BuildDataset(ctx context.Context, kind ExportKind, opts DatasetOptions) (*ExportDataset, error) {
	var (
		ds  *ExportDataset
		err error
	)
	switch kind {
	case ExportUpcomingVacations:
		ds, err = s.datasetUpcomingVacations(ctx, opts)
	case ExportStaleProfiles:
		ds, err = s.datasetStaleProfiles(ctx, opts)
	case ExportConflicts:
		ds, err = s.datasetConflicts(ctx, opts)
	case ExportAllEmployees:
		ds, err = s.datasetAllEmployees(ctx, opts)
	default:
		return nil, ErrUnknownExportKind
	}
	if err != nil {
		return nil, err
	}
	if len(opts.Columns) > 0 {
		ds = projectColumns(ds, opts.Columns)
	}
	return ds, nil
}

func projectColumns(ds *ExportDataset, cols []string) *ExportDataset {
	idx := map[string]int{}
	for i, h := range ds.Headers {
		idx[h] = i
	}
	keep := make([]int, 0, len(cols))
	newHeaders := make([]string, 0, len(cols))
	for _, c := range cols {
		i, ok := idx[c]
		if !ok {
			continue
		}
		keep = append(keep, i)
		newHeaders = append(newHeaders, c)
	}
	if len(keep) == 0 {
		return ds
	}
	newRows := make([][]any, 0, len(ds.Rows))
	for _, row := range ds.Rows {
		newRow := make([]any, 0, len(keep))
		for _, i := range keep {
			if i < len(row) {
				newRow = append(newRow, row[i])
			} else {
				newRow = append(newRow, "")
			}
		}
		newRows = append(newRows, newRow)
	}
	return &ExportDataset{
		Kind:    ds.Kind,
		Title:   ds.Title,
		Headers: newHeaders,
		Rows:    newRows,
	}
}

func (s *ExportService) DatasetToXLSX(ds *ExportDataset, filenamePrefix string) (*ExportResult, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := ds.Title
	if len(sheet) > 30 {
		sheet = sheet[:30]
	}
	if sheet == "" {
		sheet = "Отчёт"
	}
	_, _ = f.NewSheet(sheet)
	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

	writeHeaders(f, sheet, ds.Headers)
	for i, row := range ds.Rows {
		cell := fmt.Sprintf("A%d", i+2)
		copy := make([]any, len(row))
		for j, v := range row {
			copy[j] = v
		}
		_ = f.SetSheetRow(sheet, cell, &copy)
	}
	autoFitColumns(f, sheet, len(ds.Headers))

	if filenamePrefix == "" {
		filenamePrefix = "worktime-report"
	}
	name := fmt.Sprintf("%s-%s.xlsx", filenamePrefix, time.Now().Format("2006-01-02"))
	return finalize(f, name)
}

func (s *ExportService) datasetUpcomingVacations(ctx context.Context, opts DatasetOptions) (*ExportDataset, error) {
	from := time.Now().UTC()
	to := time.Now().UTC().AddDate(0, 0, 30)
	if opts.From != nil {
		from = *opts.From
	}
	if opts.To != nil {
		to = *opts.To
	}

	args := []any{from, to}
	extra := ""
	if len(opts.Departments) > 0 {
		args = append(args, opts.Departments)
		extra += fmt.Sprintf(" AND e.department = ANY($%d::text[])", len(args))
	}
	if len(opts.Kinds) > 0 {
		args = append(args, opts.Kinds)
		extra += fmt.Sprintf(" AND te.kind::text = ANY($%d::text[])", len(args))
	}
	if len(opts.RestrictEmpIDs) > 0 {
		args = append(args, opts.RestrictEmpIDs)
		extra += fmt.Sprintf(" AND e.id = ANY($%d::uuid[])", len(args))
	}

	rows, err := s.pool.Query(ctx, `
		SELECT u.full_name, u.email, u.role, COALESCE(e.department, ''),
		       te.kind, te.start_at, te.end_at, COALESCE(te.comment, '')
		FROM time_exceptions te
		JOIN employees e ON e.id = te.employee_id
		JOIN users u     ON u.id = e.user_id
		WHERE te.end_at >= $1 AND te.start_at <= $2`+extra+`
		ORDER BY te.start_at
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ds := &ExportDataset{
		Kind:    string(ExportUpcomingVacations),
		Title:   "Ближайшие отпуска и командировки",
		Headers: []string{"Сотрудник", "Email", "Роль", "Отдел", "Тип", "Начало", "Окончание", "Дней", "Комментарий"},
	}
	for rows.Next() {
		var fullName, email, role, dept, kind, comment string
		var start, end time.Time
		if err := rows.Scan(&fullName, &email, &role, &dept, &kind, &start, &end, &comment); err != nil {
			continue
		}
		days := int(end.Sub(start).Hours()/24) + 1
		ds.Rows = append(ds.Rows, []any{
			fullName, email, ruRole(role), dept,
			ruExceptionKind(kind),
			start.Format("02.01.2006 15:04"),
			end.Format("02.01.2006 15:04"),
			days, comment,
		})
	}
	return ds, nil
}

func (s *ExportService) datasetStaleProfiles(ctx context.Context, opts DatasetOptions) (*ExportDataset, error) {
	groups, err := s.diagnostics.Build(ctx)
	if err != nil {
		return nil, err
	}
	ds := &ExportDataset{
		Kind:    string(ExportStaleProfiles),
		Title:   "Сотрудники с устаревшими профилями",
		Headers: []string{"Сотрудник", "Роль", "Отдел", "TZ", "Дней без обновления", "A", "Группа", "Последнее обновление"},
	}
	all := append([]DiagnosticsRow{}, groups.Stale...)
	all = append(all, groups.NeedsConfirm...)
	all = append(all, groups.Unknown...)
	deptSet := makeStringSet(opts.Departments)
	rbacSet := opts.restrictedSet()
	for _, r := range all {
		if !inStringSet(deptSet, r.Department) {
			continue
		}
		if rbacSet != nil {
			if _, ok := rbacSet[r.EmployeeID]; !ok {
				continue
			}
		}
		lastUpd := ""
		if r.LastProfileUpdateAt != nil {
			lastUpd = r.LastProfileUpdateAt.Format("02.01.2006")
		}
		days := r.DaysSinceUpdate
		daysCell := ""
		if r.Group != "unknown" && days >= 0 {
			daysCell = fmt.Sprintf("%d", days)
		}
		ds.Rows = append(ds.Rows, []any{
			r.FullName, ruRole(r.Role), r.Department, r.Timezone,
			daysCell, fmt.Sprintf("%.2f", r.Freshness),
			ruDiagnosticsGroup(r.Group), lastUpd,
		})
	}
	return ds, nil
}

func (s *ExportService) datasetConflicts(ctx context.Context, opts DatasetOptions) (*ExportDataset, error) {
	from := time.Now().UTC().AddDate(0, 0, -7)
	to := time.Now().UTC().AddDate(0, 0, 30)
	if opts.From != nil {
		from = *opts.From
	}
	if opts.To != nil {
		to = *opts.To
	}
	list, err := s.conflicts.ListAll(ctx, from, to, 5000)
	if err != nil {
		return nil, err
	}
	ds := &ExportDataset{
		Kind:    string(ExportConflicts),
		Title:   "Конфликты в календаре",
		Headers: []string{"Сотрудник", "Отдел", "Событие", "Начало", "Окончание", "Причина", "Серьёзность"},
	}
	deptSet := makeStringSet(opts.Departments)
	rbacSet := opts.restrictedSet()
	for _, c := range list {
		if !inStringSet(deptSet, c.Department) {
			continue
		}
		if rbacSet != nil {
			if _, ok := rbacSet[c.EmployeeID.String()]; !ok {
				continue
			}
		}
		ds.Rows = append(ds.Rows, []any{
			c.FullName, c.Department, c.Title,
			c.StartAt.Format("02.01.2006 15:04"),
			c.EndAt.Format("02.01.2006 15:04"),
			ruConflictReason(c.Reason),
			ruConflictSeverity(c.Severity),
		})
	}
	return ds, nil
}

func (s *ExportService) datasetAllEmployees(ctx context.Context, opts DatasetOptions) (*ExportDataset, error) {
	args := []any{}
	conds := []string{}
	if len(opts.Departments) > 0 {
		args = append(args, opts.Departments)
		conds = append(conds, fmt.Sprintf("e.department = ANY($%d::text[])", len(args)))
	}
	if len(opts.RestrictEmpIDs) > 0 {
		args = append(args, opts.RestrictEmpIDs)
		conds = append(conds, fmt.Sprintf("e.id = ANY($%d::uuid[])", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.full_name, u.email, u.role, COALESCE(e.department, ''),
		       COALESCE(e.position, ''),
		       COALESCE(wp.timezone, u.timezone, ''),
		       COALESCE(wp.work_format::text, ''),
		       e.last_profile_update_at
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL`+where+`
		ORDER BY u.full_name
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ds := &ExportDataset{
		Kind:    string(ExportAllEmployees),
		Title:   "Сотрудники Workie",
		Headers: []string{"ФИО", "Email", "Роль", "Должность", "Отдел", "Часовой пояс", "Формат", "Последнее обновление"},
	}
	for rows.Next() {
		var (
			fullName, email, role, dept, position, tz, format string
			lastUpd                                           *time.Time
		)
		if err := rows.Scan(&fullName, &email, &role, &dept, &position, &tz, &format, &lastUpd); err != nil {
			continue
		}
		lastUpdStr := ""
		if lastUpd != nil {
			lastUpdStr = lastUpd.Format("02.01.2006")
		}
		ds.Rows = append(ds.Rows, []any{
			fullName, email, ruRole(role),
			position, dept, tz, ruWorkFormat(format), lastUpdStr,
		})
	}
	return ds, nil
}

func makeStringSet(vals []string) map[string]struct{} {
	if len(vals) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(vals))
	for _, v := range vals {
		out[v] = struct{}{}
	}
	return out
}

func inStringSet(set map[string]struct{}, v string) bool {
	if set == nil {
		return true
	}
	_, ok := set[v]
	return ok
}

func (s *ExportService) buildUpcomingVacations(ctx context.Context) (*ExportResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.full_name, u.email, u.role, COALESCE(e.department, ''),
		       te.kind, te.start_at, te.end_at, COALESCE(te.comment, '')
		FROM time_exceptions te
		JOIN employees e ON e.id = te.employee_id
		JOIN users u     ON u.id = e.user_id
		WHERE te.end_at >= now() AND te.start_at <= now() + interval '30 days'
		ORDER BY te.start_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		fullName, email, role, dept, kind, comment string
		start, end                                 time.Time
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.fullName, &r.email, &r.role, &r.dept,
			&r.kind, &r.start, &r.end, &r.comment); err != nil {
			continue
		}
		list = append(list, r)
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Отсутствия"
	_, _ = f.NewSheet(sheet)
	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

	headers := []string{"Сотрудник", "Email", "Роль", "Отдел", "Тип", "Начало", "Окончание", "Дней", "Комментарий"}
	writeHeaders(f, sheet, headers)

	for i, r := range list {
		days := int(r.end.Sub(r.start).Hours()/24) + 1
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+2), &[]any{
			r.fullName,
			r.email,
			ruRole(r.role),
			r.dept,
			ruExceptionKind(r.kind),
			r.start.Format("02.01.2006 15:04"),
			r.end.Format("02.01.2006 15:04"),
			days,
			r.comment,
		})
	}
	autoFitColumns(f, sheet, len(headers))

	return finalize(f, fmt.Sprintf("worktime-vacations-%s.xlsx", time.Now().Format("2006-01-02")))
}

func (s *ExportService) buildStaleProfiles(ctx context.Context) (*ExportResult, error) {
	groups, err := s.diagnostics.Build(ctx)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Устаревшие профили"
	_, _ = f.NewSheet(sheet)
	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

	headers := []string{"Сотрудник", "Роль", "Отдел", "TZ", "Дней без обновления", "A", "Группа", "Последнее обновление"}
	writeHeaders(f, sheet, headers)

	all := append([]DiagnosticsRow{}, groups.Stale...)
	all = append(all, groups.NeedsConfirm...)
	all = append(all, groups.Unknown...)

	for i, r := range all {
		lastUpd := ""
		if r.LastProfileUpdateAt != nil {
			lastUpd = r.LastProfileUpdateAt.Format("02.01.2006")
		}
		days := r.DaysSinceUpdate
		if r.Group == "unknown" {
			days = -1
		}
		daysCell := ""
		if days >= 0 {
			daysCell = fmt.Sprintf("%d", days)
		}
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+2), &[]any{
			r.FullName,
			ruRole(r.Role),
			r.Department,
			r.Timezone,
			daysCell,
			fmt.Sprintf("%.2f", r.Freshness),
			ruDiagnosticsGroup(r.Group),
			lastUpd,
		})
	}
	autoFitColumns(f, sheet, len(headers))

	return finalize(f, fmt.Sprintf("worktime-stale-%s.xlsx", time.Now().Format("2006-01-02")))
}

func (s *ExportService) buildConflicts(ctx context.Context) (*ExportResult, error) {
	from := time.Now().UTC().AddDate(0, 0, -7)
	to := time.Now().UTC().AddDate(0, 0, 30)
	list, err := s.conflicts.ListAll(ctx, from, to, 500)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Конфликты"
	_, _ = f.NewSheet(sheet)
	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

	headers := []string{"Сотрудник", "Отдел", "Событие", "Начало", "Окончание", "Причина", "Серьёзность"}
	writeHeaders(f, sheet, headers)

	for i, c := range list {
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+2), &[]any{
			c.FullName,
			c.Department,
			c.Title,
			c.StartAt.Format("02.01.2006 15:04"),
			c.EndAt.Format("02.01.2006 15:04"),
			ruConflictReason(c.Reason),
			ruConflictSeverity(c.Severity),
		})
	}
	autoFitColumns(f, sheet, len(headers))

	return finalize(f, fmt.Sprintf("worktime-conflicts-%s.xlsx", time.Now().Format("2006-01-02")))
}

func (s *ExportService) buildAllEmployees(ctx context.Context) (*ExportResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.full_name, u.email, u.role, COALESCE(e.department, ''),
		       COALESCE(e.position, ''),
		       COALESCE(wp.timezone, u.timezone, ''),
		       COALESCE(wp.work_format::text, ''),
		       e.last_profile_update_at
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		ORDER BY u.full_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		fullName, email, role, dept, position, tz, format string
		lastUpd                                           *time.Time
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.fullName, &r.email, &r.role, &r.dept,
			&r.position, &r.tz, &r.format, &r.lastUpd); err != nil {
			continue
		}
		list = append(list, r)
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Сотрудники"
	_, _ = f.NewSheet(sheet)
	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

	headers := []string{"ФИО", "Email", "Роль", "Должность", "Отдел", "Часовой пояс", "Формат", "Последнее обновление"}
	writeHeaders(f, sheet, headers)

	for i, r := range list {
		lastUpd := ""
		if r.lastUpd != nil {
			lastUpd = r.lastUpd.Format("02.01.2006")
		}
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+2), &[]any{
			r.fullName, r.email, ruRole(r.role),
			r.position, r.dept, r.tz, ruWorkFormat(r.format), lastUpd,
		})
	}
	autoFitColumns(f, sheet, len(headers))

	return finalize(f, fmt.Sprintf("worktime-employees-%s.xlsx", time.Now().Format("2006-01-02")))
}

func writeHeaders(f *excelize.File, sheet string, headers []string) {
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	style, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#EEF2FF"}, Pattern: 1},
		Border: []excelize.Border{
			{Type: "bottom", Color: "#C7D2FE", Style: 2},
		},
	})
	if err == nil {
		first, _ := excelize.CoordinatesToCellName(1, 1)
		last, _ := excelize.CoordinatesToCellName(len(headers), 1)
		_ = f.SetCellStyle(sheet, first, last, style)
	}
}

func autoFitColumns(f *excelize.File, sheet string, cols int) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return
	}
	for c := 0; c < cols; c++ {
		max := 8
		for _, row := range rows {
			if c >= len(row) {
				continue
			}
			l := lenRunes(row[c])
			if l > max {
				max = l
			}
		}
		colName, _ := excelize.ColumnNumberToName(c + 1)
		_ = f.SetColWidth(sheet, colName, colName, float64(max+2))
	}
}

func lenRunes(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func finalize(f *excelize.File, name string) (*ExportResult, error) {
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return &ExportResult{Filename: name, Data: buf.Bytes()}, nil
}

func ruExceptionKind(k string) string {
	switch k {
	case "vacation":
		return "Отпуск"
	case "sick_leave":
		return "Больничный"
	case "business_trip":
		return "Командировка"
	case "personal_hours":
		return "Личные часы"
	default:
		return k
	}
}

func ruDiagnosticsGroup(g string) string {
	switch g {
	case "fresh":
		return "Свежий"
	case "stale":
		return "Устаревший"
	case "needs_confirm":
		return "Нужно подтвердить"
	case "unknown":
		return "Нет данных"
	default:
		return g
	}
}

func ruConflictReason(r string) string {
	switch r {
	case "outside_hours":
		return "Вне рабочих часов"
	case "weekend":
		return "Выходной"
	case "within_exception":
		return "В период отсутствия"
	case "no_profile":
		return "Нет графика"
	default:
		return r
	}
}

func ruConflictSeverity(s string) string {
	switch strings.ToLower(s) {
	case "high":
		return "Высокая"
	case "medium":
		return "Средняя"
	case "low":
		return "Низкая"
	default:
		return s
	}
}

func ruWorkFormat(f string) string {
	switch f {
	case "office":
		return "Офис"
	case "remote":
		return "Удалённо"
	case "hybrid":
		return "Гибрид"
	default:
		return f
	}
}
