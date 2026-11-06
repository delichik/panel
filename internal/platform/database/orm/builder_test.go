package orm

import (
	"reflect"
	"testing"
)

func eq(t *testing.T, got, want string, args []any, wantArgs []any) {
	t.Helper()
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestBuilderBasic(t *testing.T) {
	q := New(nil).From("servers").Select("id", "name").
		Where("host = ?", "h1").And("port > ?", 22).
		OrderBy("created_at DESC").Limit(10).Offset(20)
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT "id", "name" FROM "servers" WHERE host = ? AND port > ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, args, []any{"h1", 22, 10, 20})
}

func TestBuilderDistinctAndExpr(t *testing.T) {
	q := New(nil).From("servers").Distinct().SelectExpr("COUNT(*)", 5).Where("name LIKE ?", "x%")
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT DISTINCT COUNT(*) FROM "servers" WHERE name LIKE ?`, args, []any{5, "x%"})
}

func TestBuilderGroups(t *testing.T) {
	q := New(nil).From("t").
		Where("a = ?", 1).
		WhereGroup(func(c *Condition) {
			c.Where("b = ?", 2).Or("c = ?", 3)
		}).
		And("d = ?", 4)
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT * FROM "t" WHERE a = ? AND (b = ? OR c = ?) AND d = ?`, args, []any{1, 2, 3, 4})
}

func TestBuilderNestedGroups(t *testing.T) {
	q := New(nil).From("t").WhereGroup(func(c *Condition) {
		c.Where("x = ?", 1).WhereGroup(func(c2 *Condition) {
			c2.Or("y = ?", 2).Or("z = ?", 3)
		})
	})
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT * FROM "t" WHERE (x = ? AND (y = ? OR z = ?))`, args, []any{1, 2, 3})
}

func TestBuilderIn(t *testing.T) {
	q := New(nil).From("t").AndIn("id", []int{1, 2, 3})
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT * FROM "t" WHERE "id" IN (?,?,?)`, args, []any{1, 2, 3})

	q2 := New(nil).From("t").OrIn("id", []any{"a", "b"})
	sql2, _ := q2.ToSQL()
	eq(t, sql2, `SELECT * FROM "t" WHERE "id" IN (?,?)`, nil, nil)

	q3 := New(nil).From("t").AndNotIn("id", []int{})
	sql3, _ := q3.ToSQL()
	eq(t, sql3, `SELECT * FROM "t" WHERE 1=1`, nil, nil)

	q4 := New(nil).From("t").AndIn("id", []int{})
	sql4, _ := q4.ToSQL()
	eq(t, sql4, `SELECT * FROM "t" WHERE 1=0`, nil, nil)
}

func TestBuilderLikeNullBetween(t *testing.T) {
	q := New(nil).From("t").
		AndLike("name", "%a%").
		OrLike("alias", "b%").
		AndNull("deleted_at").
		AndNotNull("created_at").
		AndBetween("score", 1, 9)
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT * FROM "t" WHERE "name" LIKE ? ESCAPE '\' OR "alias" LIKE ? ESCAPE '\' AND "deleted_at" IS NULL AND "created_at" IS NOT NULL AND "score" BETWEEN ? AND ?`, args, []any{"%a%", "b%", 1, 9})
}

func TestBuilderJoinsGroupHaving(t *testing.T) {
	q := New(nil).From("a").
		LeftJoin("b", `b.a_id = a.id`).
		Join("c", `c.b_id = b.id`).
		GroupBy("a.id").
		Having("COUNT(*) > ?", 3)
	sql, args := q.ToSQL()
	eq(t, sql, `SELECT * FROM "a" LEFT JOIN b ON b.a_id = a.id JOIN c ON c.b_id = b.id GROUP BY "a"."id" HAVING COUNT(*) > ?`, args, []any{3})
}

func TestBuilderRightJoin(t *testing.T) {
	q := New(nil).From("a").RightJoin("b", `b.id = a.b_id`)
	sql, _ := q.ToSQL()
	eq(t, sql, `SELECT * FROM "a" RIGHT JOIN b ON b.id = a.b_id`, nil, nil)
}

func TestBuilderSelectDefault(t *testing.T) {
	q := New(nil).From("t")
	sql, _ := q.ToSQL()
	eq(t, sql, `SELECT * FROM "t"`, nil, nil)
}
