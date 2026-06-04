package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/models"
)

const DefaultInviteMaxUses = 50

type AdminService struct {
	db     *database.DB
	config *config.Config
}

func NewAdminService(db *database.DB, cfg ...*config.Config) *AdminService {
	var resolved *config.Config
	if len(cfg) > 0 {
		resolved = cfg[0]
	}
	return &AdminService{db: db, config: resolved}
}

func (s *AdminService) ListInviteCodes() ([]models.MembershipInviteCode, error) {
	if err := ensureMembershipInviteMaxUsesColumn(s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.Conn.Query(`
		SELECT id, code, membership_level, duration_days, is_active, used_count, max_uses, note, created_by, created_at, updated_at
		FROM membership_invite_codes
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query invite codes: %w", err)
	}
	defer rows.Close()

	var results []models.MembershipInviteCode
	for rows.Next() {
		item, err := scanInviteCode(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (s *AdminService) CreateInviteCode(adminID int, req *models.CreateMembershipInviteRequest) (*models.MembershipInviteCode, error) {
	if err := ensureMembershipInviteMaxUsesColumn(s.db); err != nil {
		return nil, err
	}
	if req.MembershipLevel < 1 || req.MembershipLevel > 3 {
		return nil, fmt.Errorf("membership level must be between 1 and 3")
	}
	if req.DurationDays <= 0 {
		return nil, fmt.Errorf("duration_days must be greater than 0")
	}
	maxUses, err := resolveInviteMaxUses(req.MaxUses)
	if err != nil {
		return nil, err
	}

	code := normalizeInviteCode(req.Code)
	if code == "" {
		code = generateInviteCode()
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	row := s.db.Conn.QueryRow(`
		INSERT INTO membership_invite_codes (code, membership_level, duration_days, is_active, max_uses, note, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, code, membership_level, duration_days, is_active, used_count, max_uses, note, created_by, created_at, updated_at
	`, code, req.MembershipLevel, req.DurationDays, isActive, maxUses, req.Note, adminID)

	item, err := scanInviteCode(row)
	if err != nil {
		return nil, fmt.Errorf("failed to create invite code: %w", err)
	}
	return &item, nil
}

func (s *AdminService) SetInviteCodeActive(id int, isActive bool) (*models.MembershipInviteCode, error) {
	if err := ensureMembershipInviteMaxUsesColumn(s.db); err != nil {
		return nil, err
	}
	row := s.db.Conn.QueryRow(`
		UPDATE membership_invite_codes
		SET is_active = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		RETURNING id, code, membership_level, duration_days, is_active, used_count, max_uses, note, created_by, created_at, updated_at
	`, isActive, id)

	item, err := scanInviteCode(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invite code not found")
		}
		return nil, fmt.Errorf("failed to update invite code: %w", err)
	}
	return &item, nil
}

func (s *AdminService) ListSystemStrategies() ([]models.StrategyParams, error) {
	rows, err := s.db.Conn.Query(`
		SELECT id, unique_key, user_id, name, is_public,
		       buy_threshold_pct, sell_threshold_pct, initial_cash,
		       enable_rebalance, max_position_pct, min_position_pct,
		       slope_position_per_pct, rebalance_tolerance_pct,
		       trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
		FROM mtf_strategy_params
		WHERE is_public = 1
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query system strategies: %w", err)
	}
	defer rows.Close()

	var results []models.StrategyParams
	for rows.Next() {
		var item models.StrategyParams
		var uid sql.NullInt64
		err := rows.Scan(
			&item.ID, &item.UniqueKey, &uid, &item.Name, &item.IsPublic,
			&item.BuyThresholdPct, &item.SellThresholdPct, &item.InitialCash,
			&item.EnableRebalance, &item.MaxPositionPct, &item.MinPositionPct,
			&item.SlopePositionPerPct, &item.RebalanceTolerancePct,
			&item.TradeFeeRate, &item.TakeProfitThresholdPct, &item.TakeProfitSellFrac,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan system strategy: %w", err)
		}
		if uid.Valid {
			v := int(uid.Int64)
			item.UserID = &v
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (s *AdminService) SaveSystemStrategy(req *models.SaveStrategyParamsRequest) (*models.StrategyParams, error) {
	req.UniqueKey = strings.TrimSpace(req.UniqueKey)
	if req.UniqueKey == "" {
		return nil, fmt.Errorf("unique_key is required")
	}
	_, err := s.db.Conn.Exec(`
		INSERT INTO mtf_strategy_params (
			unique_key, user_id, name, is_public,
			buy_threshold_pct, sell_threshold_pct, initial_cash,
			enable_rebalance, max_position_pct, min_position_pct,
			slope_position_per_pct, rebalance_tolerance_pct,
			trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
		) VALUES (
			$1, NULL, $2, 1,
			$3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12, $13
		)
		ON CONFLICT (unique_key) DO UPDATE SET
			user_id = NULL,
			name = EXCLUDED.name,
			is_public = 1,
			buy_threshold_pct = EXCLUDED.buy_threshold_pct,
			sell_threshold_pct = EXCLUDED.sell_threshold_pct,
			initial_cash = EXCLUDED.initial_cash,
			enable_rebalance = EXCLUDED.enable_rebalance,
			max_position_pct = EXCLUDED.max_position_pct,
			min_position_pct = EXCLUDED.min_position_pct,
			slope_position_per_pct = EXCLUDED.slope_position_per_pct,
			rebalance_tolerance_pct = EXCLUDED.rebalance_tolerance_pct,
			trade_fee_rate = EXCLUDED.trade_fee_rate,
			take_profit_threshold_pct = EXCLUDED.take_profit_threshold_pct,
			take_profit_sell_frac = EXCLUDED.take_profit_sell_frac,
			updated_at = CURRENT_TIMESTAMP
	`, req.UniqueKey, req.Name,
		req.BuyThresholdPct, req.SellThresholdPct, req.InitialCash,
		req.EnableRebalance, req.MaxPositionPct, req.MinPositionPct,
		req.SlopePositionPerPct, req.RebalanceTolerancePct,
		req.TradeFeeRate, req.TakeProfitThresholdPct, req.TakeProfitSellFrac,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to save system strategy: %w", err)
	}
	return s.getSystemStrategyByUniqueKey(req.UniqueKey)
}

func (s *AdminService) GetGatewayQueueStatus() (*models.AdminGatewayQueueStatus, error) {
	candidates := s.gatewayBaseURLCandidates()
	if len(candidates) == 0 {
		return &models.AdminGatewayQueueStatus{
			Reachable:   false,
			Status:      "unconfigured",
			Jobs:        map[string]int{},
			Backends:    []models.AdminGatewayBackendStatus{},
			Error:       "gateway url is not configured",
			CheckedPath: "/health",
		}, nil
	}

	client := newPythonServiceHTTPClient(s.gatewayTimeoutSeconds())
	attemptErrors := make([]string, 0, len(candidates))
	for _, baseURL := range candidates {
		requestURL := strings.TrimRight(baseURL, "/") + "/health"
		req, err := http.NewRequest(http.MethodGet, requestURL, nil)
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", requestURL, err))
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", requestURL, err))
			continue
		}
		status, decodeErr := decodeGatewayHealthResponse(resp, requestURL)
		if decodeErr != nil {
			attemptErrors = append(attemptErrors, decodeErr.Error())
			continue
		}
		return status, nil
	}

	return &models.AdminGatewayQueueStatus{
		Reachable:   false,
		Status:      "unreachable",
		Jobs:        map[string]int{},
		Backends:    []models.AdminGatewayBackendStatus{},
		Error:       strings.Join(attemptErrors, " | "),
		CheckedPath: "/health",
	}, nil
}

func (s *AdminService) getSystemStrategyByUniqueKey(uniqueKey string) (*models.StrategyParams, error) {
	row := s.db.Conn.QueryRow(`
		SELECT id, unique_key, user_id, name, is_public,
		       buy_threshold_pct, sell_threshold_pct, initial_cash,
		       enable_rebalance, max_position_pct, min_position_pct,
		       slope_position_per_pct, rebalance_tolerance_pct,
		       trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
		FROM mtf_strategy_params
		WHERE unique_key = $1 AND is_public = 1
		LIMIT 1
	`, uniqueKey)
	var item models.StrategyParams
	var uid sql.NullInt64
	err := row.Scan(
		&item.ID, &item.UniqueKey, &uid, &item.Name, &item.IsPublic,
		&item.BuyThresholdPct, &item.SellThresholdPct, &item.InitialCash,
		&item.EnableRebalance, &item.MaxPositionPct, &item.MinPositionPct,
		&item.SlopePositionPerPct, &item.RebalanceTolerancePct,
		&item.TradeFeeRate, &item.TakeProfitThresholdPct, &item.TakeProfitSellFrac,
	)
	if err != nil {
		return nil, err
	}
	if uid.Valid {
		v := int(uid.Int64)
		item.UserID = &v
	}
	return &item, nil
}

func (s *AdminService) gatewayTimeoutSeconds() int {
	if s != nil && s.config != nil && s.config.PythonService.Timeout > 0 && s.config.PythonService.Timeout < 5 {
		return s.config.PythonService.Timeout
	}
	return 5
}

func (s *AdminService) gatewayBaseURLCandidates() []string {
	if s == nil || s.config == nil {
		return nil
	}

	values := make([]string, 0, 5)
	addCandidate := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range values {
			if strings.EqualFold(existing, raw) {
				return
			}
		}
		values = append(values, raw)
	}

	primary := strings.TrimSpace(s.config.UZI.QueueBaseURL)
	if primary == "" {
		primary = strings.TrimSpace(s.config.PythonService.BaseURL)
	}
	addCandidate(primary)

	parsed, err := url.Parse(primary)
	if err != nil {
		return values
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}

	switch strings.ToLower(parsed.Hostname()) {
	case "host.docker.internal", "127.0.0.1", "localhost":
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59010", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59010", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59010", scheme))
		addCandidate(fmt.Sprintf("%s://ai-functions-gateway:9010", scheme))
	case "ai-functions-gateway":
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59010", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59010", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59010", scheme))
	default:
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59010", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59010", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59010", scheme))
		addCandidate(fmt.Sprintf("%s://ai-functions-gateway:9010", scheme))
	}

	return values
}

func decodeGatewayHealthResponse(resp *http.Response, requestURL string) (*models.AdminGatewayQueueStatus, error) {
	defer resp.Body.Close()
	body, err := readPythonJSONResponse(resp, requestURL, "decode gateway health response")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s: gateway health returned status=%d", requestURL, resp.StatusCode)
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal gateway health response: %w", err)
	}

	var parsed struct {
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
		Scheduler struct {
			QueueDepth int                                `json:"queue_depth"`
			Jobs       map[string]int                     `json:"jobs"`
			Backends   []models.AdminGatewayBackendStatus `json:"backends"`
		} `json:"scheduler"`
	}
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return nil, fmt.Errorf("parse gateway health response: %w", err)
	}
	if parsed.Scheduler.Jobs == nil {
		parsed.Scheduler.Jobs = map[string]int{}
	}
	if parsed.Scheduler.Backends == nil {
		parsed.Scheduler.Backends = []models.AdminGatewayBackendStatus{}
	}

	return &models.AdminGatewayQueueStatus{
		Reachable:   true,
		Status:      parsed.Status,
		Timestamp:   parsed.Timestamp,
		SourceURL:   requestURL,
		QueueDepth:  parsed.Scheduler.QueueDepth,
		Jobs:        parsed.Scheduler.Jobs,
		Backends:    parsed.Scheduler.Backends,
		CheckedPath: "/health",
	}, nil
}

type inviteScanner interface {
	Scan(dest ...interface{}) error
}

func scanInviteCode(scanner inviteScanner) (models.MembershipInviteCode, error) {
	var item models.MembershipInviteCode
	err := scanner.Scan(
		&item.ID, &item.Code, &item.MembershipLevel, &item.DurationDays,
		&item.IsActive, &item.UsedCount, &item.MaxUses, &item.Note, &item.CreatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}
	return item, nil
}

func resolveInviteMaxUses(maxUses *int) (int, error) {
	if maxUses == nil {
		return DefaultInviteMaxUses, nil
	}
	if *maxUses <= 0 {
		return 0, fmt.Errorf("max_uses must be greater than 0")
	}
	return *maxUses, nil
}

func normalizeInviteCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
}

func generateInviteCode() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "VIP" + strings.ReplaceAll(strings.ToUpper(fmt.Sprintf("%08x", buf)), " ", "")
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return "VIP" + encoded[:10]
}
