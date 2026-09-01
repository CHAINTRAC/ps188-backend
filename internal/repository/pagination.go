package repository

import "github.com/sih26/ps188-backend/internal/response"

// pageOf trims the sentinel (limit+1) row, maps DB rows to views, and derives
// the next cursor. rows must be sorted newest-first by _id.
func pageOf[DB any, V any](
	rows []DB,
	limit int64,
	toView func(DB) V,
	idOf func(DB) string,
) response.Page[V] {
	hasNext := int64(len(rows)) > limit
	if hasNext {
		rows = rows[:limit]
	}
	views := make([]V, 0, len(rows))
	for _, row := range rows {
		views = append(views, toView(row))
	}
	info := response.PageInfo{HasNext: hasNext}
	if hasNext && len(rows) > 0 {
		info.NextCursor = idOf(rows[len(rows)-1])
	}
	return response.Page[V]{Data: views, Info: info}
}
