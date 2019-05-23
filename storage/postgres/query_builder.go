package postgres

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/Peripli/service-manager/storage"

	"github.com/Peripli/service-manager/pkg/query"
	"github.com/jmoiron/sqlx"
)

type QueryOperation string

const (
	ListOperation QueryOperation = "list"
)

type QueryBuilder struct {
	db     pgDB
	entity PostgresEntity

	SQL         string
	queryParams []interface{}

	labelCriteria, fieldCriteria []query.Criterion
	orderByFields                []string
	limit                        int
	criteria                     []query.Criterion
	hasLock                      bool

	err error
}

func newQueryBuilder(db pgDB, entity PostgresEntity) *QueryBuilder {
	return &QueryBuilder{
		db:     db,
		entity: entity,
	}
}

func (qb *QueryBuilder) List(ctx context.Context) (*sqlx.Rows, error) {
	if qb.err != nil {
		return nil, qb.err
	}

	baseTableName := qb.entity.TableName()
	var baseQuery string
	label := qb.entity.LabelEntity()
	if label == nil {
		baseQuery = constructBaseQueryForEntity(baseTableName)
	} else {
		baseQuery = constructBaseQueryForLabelable(label, baseTableName)
	}
	qb.SQL = baseQuery

	qb.withLabelCriteria(qb.labelCriteria).withFieldCriteria(qb.fieldCriteria)
	if qb.err != nil {
		return nil, qb.err
	}

	if hasMultiVariateOp(qb.criteria) {
		var err error
		// sqlx.In requires question marks(?) instead of positional arguments (the ones pgsql uses) in order to map the list argument to the IN operation
		if qb.SQL, qb.queryParams, err = sqlx.In(qb.SQL, qb.queryParams...); err != nil {
			qb.err = err
			return nil, err
		}
	}
	qb.SQL = qb.db.Rebind(qb.SQL)

	qb.buildOrderBy().addLimit().addLock()
	qb.SQL += ";"

	return qb.db.QueryxContext(ctx, qb.SQL, qb.queryParams...)
}

func (qb *QueryBuilder) WithListCriteria(criteria ...storage.ListCriteria) *QueryBuilder {
	if qb.err != nil {
		return qb
	}

	for _, c := range criteria {
		if c.Type == storage.OrderByCriteriaType {
			qb.orderByFields = append(qb.orderByFields, c.Parameter.(string))
		}
		if c.Type == storage.LimitCriteriaType {
			limit := c.Parameter.(int)
			if limit <= 0 {
				qb.err = fmt.Errorf("limit (%d) should be greater than 0", limit)
			}
			qb.limit = limit
		}
	}

	return qb
}

func (qb *QueryBuilder) WithCriteria(criteria ...query.Criterion) *QueryBuilder {
	if qb.err != nil {
		return qb
	}

	if len(criteria) == 0 {
		return qb
	}

	qb.criteria = append(qb.criteria, criteria...)

	if err := validateFieldQueryParams(getDBTags(qb.entity, nil), qb.criteria); err != nil {
		qb.err = err
		return qb
	}

	qb.labelCriteria, qb.fieldCriteria = splitCriteriaByType(criteria)

	return qb
}

func (qb *QueryBuilder) WithLock() *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if _, ok := qb.db.(*sqlx.Tx); ok {
		qb.hasLock = true
	}
	return qb
}

func (qb *QueryBuilder) buildOrderBy() *QueryBuilder {
	if len(qb.orderByFields) > 0 {
		orderFields := strings.Join(qb.orderByFields, ",")
		qb.SQL += fmt.Sprintf(" ORDER BY %s", orderFields)
	}
	return qb
}

func (qb *QueryBuilder) addLimit() *QueryBuilder {
	if qb.limit > 0 {
		qb.SQL += fmt.Sprintf(" LIMIT %d", qb.limit)
	}
	return qb
}

func (qb *QueryBuilder) addLock() *QueryBuilder {
	if qb.hasLock {
		// Lock the rows if we are in transaction so that update operations on those rows can rely on unchanged data
		// This allows us to handle concurrent updates on the same rows by executing them sequentially as
		// before updating we have to anyway select the rows and can therefore lock them
		qb.SQL += fmt.Sprintf(" FOR SHARE of %s", qb.entity.TableName())
	}
	return qb
}

func (qb *QueryBuilder) withLabelCriteria(criteria []query.Criterion) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	var labelQueries []string

	labelEntity := qb.entity.LabelEntity()
	if len(criteria) > 0 {
		labelTableName := labelEntity.LabelsTableName()
		referenceColumnName := labelEntity.ReferenceColumn()
		labelSubQuery := fmt.Sprintf("(SELECT * FROM %[1]s WHERE %[2]s IN (SELECT %[2]s FROM %[1]s WHERE ", labelTableName, referenceColumnName)
		for _, option := range criteria {
			rightOpBindVar, rightOpQueryValue := buildRightOp(option)
			sqlOperation := translateOperationToSQLEquivalent(option.Operator)
			labelQueries = append(labelQueries, fmt.Sprintf("(%[1]s.key = ? AND %[1]s.val %[2]s %s)", labelTableName, sqlOperation, rightOpBindVar))
			qb.queryParams = append(qb.queryParams, option.LeftOp, rightOpQueryValue)
		}
		labelSubQuery += strings.Join(labelQueries, " OR ")
		labelSubQuery += "))"

		qb.SQL = strings.Replace(qb.SQL, "LEFT JOIN", "JOIN "+labelSubQuery, 1)
	}
	return qb
}

func (qb *QueryBuilder) withFieldCriteria(criteria []query.Criterion) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	baseTableName := qb.entity.TableName()
	dbTags := getDBTags(qb.entity, nil)

	var fieldQueries []string

	if len(criteria) > 0 {
		qb.SQL += " WHERE "
		for _, option := range criteria {
			var ttype reflect.Type
			if dbTags != nil {
				var err error
				ttype, err = findTagType(dbTags, option.LeftOp)
				if err != nil {
					qb.err = err
					return qb
				}
			}
			rightOpBindVar, rightOpQueryValue := buildRightOp(option)
			sqlOperation := translateOperationToSQLEquivalent(option.Operator)

			dbCast := determineCastByType(ttype)
			clause := fmt.Sprintf("%s.%s%s %s %s", baseTableName, option.LeftOp, dbCast, sqlOperation, rightOpBindVar)
			if option.Operator.IsNullable() {
				clause = fmt.Sprintf("(%s OR %s.%s IS NULL)", clause, baseTableName, option.LeftOp)
			}
			fieldQueries = append(fieldQueries, clause)
			qb.queryParams = append(qb.queryParams, rightOpQueryValue)
		}
		qb.SQL += strings.Join(fieldQueries, " AND ")
	}
	return qb
}
