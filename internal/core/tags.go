package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Ordering ports Model\Ordering.
type Ordering struct {
	Field     *string
	Direction string
}

// OrderingFromOptionalString ports Ordering::fromOptionalString() (parseOrderBy splits on '-').
func OrderingFromOptionalString(v *string) Ordering {
	if v == nil {
		return Ordering{Direction: "ASC"}
	}
	field, dir, hasDir := strings.Cut(*v, "-")
	if i := strings.Index(dir, "-"); i >= 0 {
		dir = dir[:i]
	}
	o := Ordering{Field: &field, Direction: "ASC"}
	if hasDir {
		o.Direction = dir
	}
	return o
}

// orderDirection validates a direction the way the SQL layer upstream does: anything but ASC/DESC fails the
// query, which surfaces as an internal server error.
func orderDirection(o Ordering) (string, error) {
	dir := strings.ToUpper(o.Direction)
	if dir != "ASC" && dir != "DESC" {
		return "", fmt.Errorf("invalid order direction %q", o.Direction)
	}
	return dir, nil
}

// TagsParams ports Tag\Model\TagsParams.
type TagsParams struct {
	SearchTerm   *string
	OrderBy      Ordering
	Page         int
	ItemsPerPage int
}

// TagInfo ports Tag\Model\TagInfo.
type TagInfo struct {
	Tag            string
	ShortURLsCount int
	VisitsSummary  VisitsSummary
}

// EncodePHP serializes like TagInfo::jsonSerialize().
func (t TagInfo) EncodePHP(int) any {
	return phpx.Map{
		{Key: "tag", Value: t.Tag},
		{Key: "shortUrlsCount", Value: t.ShortURLsCount},
		{Key: "visitsSummary", Value: t.VisitsSummary},
	}
}

// TagService ports Tag\TagService.
type TagService struct {
	Store *Store
}

// tagsBase returns FROM/WHERE for tags, joined with short URLs when the API key restricts them
// (WithApiKeySpecsEnsuringJoin) and filtered by search term.
func tagsBase(k *APIKey, searchTerm *string) (string, []any) {
	from := " FROM tags t"
	var conds []string
	var args []any
	if IsShortURLRestricted(k) {
		from += " INNER JOIN short_urls_in_tags st ON st.tag_id = t.id INNER JOIN short_urls s ON s.id = st.short_url_id"
		conds, args = apiKeyShortURLConditions(k, "s")
	}
	if searchTerm != nil {
		conds = append(conds, "t.name LIKE ?")
		args = append(args, "%"+*searchTerm+"%")
	}
	return from + where(conds), args
}

func (t *TagService) countTags(ctx context.Context, p TagsParams, k *APIKey) (int, error) {
	from, args := tagsBase(k, p.SearchTerm)
	var n db.Int
	err := t.Store.DB.QueryRow(ctx, t.Store.DB.DB, "SELECT COUNT(DISTINCT t.id)"+from, args...).Scan(&n)
	return int(n), err
}

// ListTags ports TagService::listTags().
func (t *TagService) ListTags(ctx context.Context, p TagsParams, k *APIKey) (*Paginator[string], error) {
	return NewPaginator[string](ctx, FuncAdapter[string]{
		CountFn: func(ctx context.Context) (int, error) { return t.countTags(ctx, p, k) },
		SliceFn: func(ctx context.Context, offset, length int) ([]string, error) {
			from, args := tagsBase(k, p.SearchTerm)
			dir, err := orderDirection(p.OrderBy)
			if err != nil {
				return nil, err
			}
			query := t.Store.DB.Limit("SELECT t.id, t.name"+from+" ORDER BY t.name "+dir, &length, &offset)
			rows, err := t.Store.DB.Query(ctx, t.Store.DB.DB, query, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			seen := map[int64]bool{}
			out := []string{}
			for rows.Next() {
				var id int64
				var name string
				if err := rows.Scan(&id, &name); err != nil {
					return nil, err
				}
				// The ORM hydrates each entity once even if the join repeats it
				if !seen[id] {
					seen[id] = true
					out = append(out, name)
				}
			}
			return out, rows.Err()
		},
	}, p.ItemsPerPage, p.Page)
}

var tagOrderFields = map[string]string{"tag": "tag", "shortUrlsCount": "short_urls_count", "visits": "visits", "nonBotVisits": "non_bot_visits"}

// TagsInfo ports TagService::tagsInfo() and TagRepository::findTagsWithInfo().
func (t *TagService) TagsInfo(ctx context.Context, p TagsParams, k *APIKey) (*Paginator[TagInfo], error) {
	return NewPaginator[TagInfo](ctx, FuncAdapter[TagInfo]{
		CountFn: func(ctx context.Context) (int, error) { return t.countTags(ctx, p, k) },
		SliceFn: func(ctx context.Context, offset, length int) ([]TagInfo, error) {
			return t.Store.findTagsWithInfo(ctx, p, k, length, offset)
		},
	}, p.ItemsPerPage, p.Page)
}

func (s *Store) findTagsWithInfo(ctx context.Context, p TagsParams, k *APIKey, limit, offset int) ([]TagInfo, error) {
	orderField := "tag"
	if p.OrderBy.Field != nil {
		if f, ok := tagOrderFields[*p.OrderBy.Field]; ok {
			orderField = f
		}
	}
	dir, err := orderDirection(p.OrderBy)
	if err != nil {
		return nil, err
	}
	restricted := IsShortURLRestricted(k)
	join := "LEFT JOIN"
	if restricted {
		join = "INNER JOIN"
	}
	keyConds := func() ([]string, []any) {
		return apiKeyShortURLConditions(k, "s")
	}
	tagConds, tagArgs := keyConds()
	if p.SearchTerm != nil {
		tagConds = append(tagConds, "t.name LIKE ?")
		tagArgs = append(tagArgs, "%"+*p.SearchTerm+"%")
	}
	tagsSub := "SELECT t.id AS tag_id, t.name AS tag, COUNT(DISTINCT s.id) AS short_urls_count FROM tags t " + join +
		" short_urls_in_tags st ON st.tag_id = t.id " + join + " short_urls s ON st.short_url_id = s.id" + where(tagConds) +
		" GROUP BY t.id, t.name"
	visitsSub := func(excludeBots bool, alias string) (string, []any) {
		joinCond := "sc.short_url_id = st.short_url_id"
		if excludeBots {
			joinCond += " AND sc.potential_bot = '0'"
		}
		conds, args := keyConds()
		return "SELECT st.tag_id AS tag_id, SUM(sc.count) AS " + alias + " FROM short_url_visits_counts sc INNER JOIN short_urls_in_tags st ON " +
			joinCond + " INNER JOIN short_urls s ON sc.short_url_id = s.id" + where(conds) + " GROUP BY st.tag_id", args
	}
	allSub, allArgs := visitsSub(false, "visits")
	botSub, botArgs := visitsSub(true, "non_bot_visits")
	query := "SELECT t.tag AS tag, COALESCE(v.visits, 0) AS visits, COALESCE(b.non_bot_visits, 0) AS non_bot_visits, " +
		"COALESCE(t.short_urls_count, 0) AS short_urls_count FROM (" + tagsSub + ") t LEFT JOIN (" + allSub +
		") v ON t.tag_id = v.tag_id LEFT JOIN (" + botSub + ") b ON t.tag_id = b.tag_id ORDER BY " + orderField + " " + dir
	if orderField != "tag" {
		query += ", tag ASC"
	}
	query = s.DB.Limit(query, &limit, &offset)
	args := append(append(tagArgs, allArgs...), botArgs...)
	rows, err := s.DB.Query(ctx, s.DB.DB, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagInfo{}
	for rows.Next() {
		var name string
		var visits, nonBots, count db.Int
		if err := rows.Scan(&name, &visits, &nonBots, &count); err != nil {
			return nil, err
		}
		out = append(out, TagInfo{Tag: name, ShortURLsCount: int(count), VisitsSummary: VisitsSummary{Total: int(visits), NonBots: int(nonBots)}})
	}
	return out, rows.Err()
}

// TagExists ports TagRepository::tagExists().
func (s *Store) TagExists(ctx context.Context, tag string, k *APIKey) (bool, error) {
	from := " FROM tags t"
	conds := []string{"t.name = ?"}
	args := []any{tag}
	if IsShortURLRestricted(k) {
		from += " INNER JOIN short_urls_in_tags st ON st.tag_id = t.id INNER JOIN short_urls s ON s.id = st.short_url_id"
		kc, ka := apiKeyShortURLConditions(k, "s")
		conds = append(conds, kc...)
		args = append(args, ka...)
	}
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COUNT(t.id)"+from+where(conds), args...).Scan(&n)
	return n > 0, err
}

// DeleteTags ports TagService::deleteTags().
func (t *TagService) DeleteTags(ctx context.Context, names []string, k *APIKey) error {
	if IsShortURLRestricted(k) {
		return ErrForbiddenTagDeletion()
	}
	if len(names) == 0 {
		return nil
	}
	args := make([]any, len(names))
	for i, n := range names {
		args[i] = n
	}
	return t.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		// Join rows go first: SQLite does not enforce the ON DELETE CASCADE upstream relies on elsewhere
		if _, err := t.Store.DB.Exec(ctx, tx, "DELETE FROM short_urls_in_tags WHERE tag_id IN (SELECT id FROM tags WHERE name IN ("+
			placeholders(len(names))+"))", args...); err != nil {
			return err
		}
		_, err := t.Store.DB.Exec(ctx, tx, "DELETE FROM tags WHERE name IN ("+placeholders(len(names))+")", args...)
		return err
	})
}

// RenameTag ports TagService::renameTag().
func (t *TagService) RenameTag(ctx context.Context, r Renaming, k *APIKey) error {
	if IsShortURLRestricted(k) {
		return ErrForbiddenTagRenaming()
	}
	var id int64
	one := 1
	err := t.Store.DB.QueryRow(ctx, t.Store.DB.DB, t.Store.DB.Limit("SELECT id FROM tags WHERE name = ? ORDER BY id", &one, nil), r.OldName).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTagNotFound(r.OldName)
	}
	if err != nil {
		return err
	}
	if r.NameChanged() {
		var n db.Int
		if err := t.Store.DB.QueryRow(ctx, t.Store.DB.DB, "SELECT COUNT(*) FROM tags WHERE name = ?", r.NewName).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrTagConflict(r)
		}
	}
	_, err = t.Store.DB.Exec(ctx, t.Store.DB.DB, "UPDATE tags SET name = ? WHERE id = ?", r.NewName, id)
	return err
}
