package postgres

import (
	"context"
	"strconv"

	"LLMGateway/server/internal/db/sqlc"
	domain "LLMGateway/server/internal/ratelimit"

	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) ListRateLimits(ctx context.Context, ownerUserID int, enabled *bool, page, pageSize int) (domain.ListResponse[domain.RateLimitRuleDTO], error) {
	limit, offset := limitOffset(page, pageSize)

	var enabledArg pgtype.Bool
	if enabled != nil {
		enabledArg = pgtype.Bool{Bool: *enabled, Valid: true}
	}

	rows, err := s.queries.ListRateLimitRules(ctx, sqlc.ListRateLimitRulesParams{OwnerUserID: int64(ownerUserID), PageLimit: limit, PageOffset: offset, Enabled: enabledArg})
	if err != nil {
		return domain.ListResponse[domain.RateLimitRuleDTO]{}, mapError(err)
	}
	total, err := s.queries.CountRateLimitRules(ctx, sqlc.CountRateLimitRulesParams{OwnerUserID: int64(ownerUserID), Enabled: enabledArg})
	if err != nil {
		return domain.ListResponse[domain.RateLimitRuleDTO]{}, mapError(err)
	}

	list := []domain.RateLimitRuleDTO{}
	for _, row := range rows {
		list = append(list, domain.RateLimitRuleToDTO(rateLimitRule(row.ID, row.RuleName, row.TargetType, row.TargetValue, row.Metric, row.LimitValue, row.Action, row.Priority, row.Enabled, row.Extras)))
	}
	return domain.ListResponse[domain.RateLimitRuleDTO]{List: list, Total: int(total)}, nil
}

func (s *Store) GetRateLimit(ctx context.Context, ownerUserID, id int) (domain.RateLimitRule, error) {
	row, err := s.queries.GetRateLimitRule(ctx, sqlc.GetRateLimitRuleParams{ID: int64(id), OwnerUserID: int64(ownerUserID)})
	if err != nil {
		return domain.RateLimitRule{}, mapError(err)
	}
	return rateLimitRule(row.ID, row.RuleName, row.TargetType, row.TargetValue, row.Metric, row.LimitValue, row.Action, row.Priority, row.Enabled, row.Extras), nil
}

func (s *Store) InsertRateLimit(ctx context.Context, ownerUserID int, rule domain.RateLimitRule) (int, error) {
	id, err := s.queries.CreateRateLimitRule(ctx, sqlc.CreateRateLimitRuleParams{
		OwnerUserID: int64(ownerUserID),
		RuleName:    rule.RuleName,
		TargetType:  rule.TargetType,
		TargetValue: rule.TargetValue,
		Metric:      rule.Metric,
		LimitValue:  rule.LimitValue,
		Action:      rule.Action,
		Priority:    int32(rule.Priority),
		Enabled:     rule.Enabled,
		Extras:      rule.Extras,
	})
	if err != nil {
		return 0, mapError(err)
	}
	return int(id), nil
}

func (s *Store) UpdateRateLimitRecord(ctx context.Context, ownerUserID, id int, rule domain.RateLimitRule) (bool, error) {
	affected, err := s.queries.UpdateRateLimitRule(ctx, sqlc.UpdateRateLimitRuleParams{
		RuleName:    rule.RuleName,
		TargetType:  rule.TargetType,
		TargetValue: rule.TargetValue,
		Metric:      rule.Metric,
		LimitValue:  rule.LimitValue,
		Action:      rule.Action,
		Priority:    int32(rule.Priority),
		Enabled:     rule.Enabled,
		Extras:      rule.Extras,
		ID:          int64(id),
		OwnerUserID: int64(ownerUserID),
	})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) DeleteRateLimit(ctx context.Context, ownerUserID, id int) (bool, error) {
	affected, err := s.queries.DeleteRateLimitRule(ctx, sqlc.DeleteRateLimitRuleParams{ID: int64(id), OwnerUserID: int64(ownerUserID)})
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

// TargetOwnedByUser checks that a rule target references a resource owned by
// ownerUserID. "*" is always allowed; a user target must equal the owner.
func (s *Store) TargetOwnedByUser(ctx context.Context, ownerUserID int, targetType, targetValue string) (bool, error) {
	if targetValue == "*" {
		return true, nil
	}
	var exists bool
	switch targetType {
	case "user":
		return targetValue == strconv.Itoa(ownerUserID), nil
	case "api_key":
		id, err := strconv.Atoi(targetValue)
		if err != nil {
			return false, nil
		}
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM client_api_keys WHERE id = $1 AND user_id = $2)`, id, ownerUserID).Scan(&exists); err != nil {
			return false, mapError(err)
		}
	case "channel":
		id, err := strconv.Atoi(targetValue)
		if err != nil {
			return false, nil
		}
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channels WHERE id = $1 AND owner_user_id = $2)`, id, ownerUserID).Scan(&exists); err != nil {
			return false, mapError(err)
		}
	case "model":
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_models cm JOIN channels c ON c.id = cm.channel_id WHERE cm.model_name = $1 AND c.owner_user_id = $2)`, targetValue, ownerUserID).Scan(&exists); err != nil {
			return false, mapError(err)
		}
	default:
		return false, nil
	}
	return exists, nil
}

func rateLimitRule(id int64, ruleName, targetType, targetValue, metric string, limitValue int64, action string, priority int32, enabled bool, extras []byte) domain.RateLimitRule {
	return domain.RateLimitRule{ID: int(id), RuleName: ruleName, TargetType: targetType, TargetValue: targetValue, Metric: metric, LimitValue: limitValue, Action: action, Priority: int(priority), Enabled: enabled, Extras: extras}
}
