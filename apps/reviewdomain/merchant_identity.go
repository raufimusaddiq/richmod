package reviewdomain

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrAmbiguousMerchantMatch = errors.New("reviewdomain: normalized payee matches more than one merchant")
var ErrUnusableMerchantName = errors.New("reviewdomain: merchant name cannot be used")

type MerchantQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

var payeeInstitutionSuffixes = []string{
	"bank syariah indonesia",
	"bank syariah mandiri",
	"bank central asia",
	"bank rakyat indonesia",
	"bank negara indonesia",
	"bank tabungan negara",
	"bank cimb niaga",
	"bank sinarmas",
	"bank maybank",
	"bank muamalat",
	"bank bukopin",
	"bank permata",
	"bank danamon",
	"bank seabank",
	"bank mandiri",
	"bank panin",
	"bank bjb",
	"bank dki",
	"bank mega",
	"bank jago",
	"bank btpn",
	"bank ocbc",
	"bank bca",
	"bank bri",
	"bank bni",
	"bank btn",
	"bank bsi",
	"bank aceh",
	"bank sumut",
	"bank nagari",
	"bank kalsel",
	"bank kaltim",
	"bank kaltara",
	"bank kalteng",
	"bank kalbar",
	"bank jateng",
	"bank jatim",
	"bank bali",
	"bank ntb",
	"bank ntt",
	"bank lampung",
	"bank riau",
	"bank jambi",
	"bank bengkulu",
	"bank banten",
	"bank sulselbar",
	"bank sulsel",
	"bank sulbar",
	"bank sulutgo",
	"bank sulteng",
	"bank sultra",
	"bank papua",
	"bank maluku utara",
	"bank maluku",
	"cimb niaga",
	"shopeepay",
	"shopee pay",
	"linkaja",
	"motionpay",
	"astrapay",
	"sakuku",
	"jenius",
	"isaku",
	"seabank",
	"mandiri",
	"permata",
	"danamon",
	"sinarmas",
	"maybank",
	"muamalat",
	"bukopin",
	"gopay",
	"go pay",
	"ocbc",
	"panin",
	"cimb",
	"btpn",
	"jago",
	"bjb",
	"bca",
	"bri",
	"bni",
	"btn",
	"bsi",
	"ovo",
	"dana",
}

func NormalizePayeeName(raw string) string {
	name := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	withoutAccount := stripTrailingAccountTail(name)
	if withoutAccount == name {
		return name
	}
	name = withoutAccount
	strippedInstitution := false
	for i := 0; i < 4; i++ {
		next := stripInstitutionSuffix(name)
		if next == name {
			break
		}
		strippedInstitution = true
		name = next
	}
	if strippedInstitution && len(strings.Fields(name)) < 2 {
		return ""
	}
	return name
}

func stripInstitutionSuffix(name string) string {
	lower := strings.ToLower(name)
	best := ""
	for _, suffix := range payeeInstitutionSuffixes {
		if len(suffix) <= len(best) {
			continue
		}
		if lower == suffix || strings.HasSuffix(lower, " "+suffix) {
			best = suffix
		}
	}
	if best == "" {
		return name
	}
	return strings.Join(strings.Fields(strings.TrimSpace(name[:len(name)-len(best)])), " ")
}

func stripTrailingAccountTail(name string) string {
	fields := strings.Fields(name)
	groups := 0
	for i := len(fields) - 1; i >= 0; i-- {
		if !accountDigitGroup(fields[i]) {
			break
		}
		groups++
	}
	if groups == 0 {
		return name
	}
	if groups == 1 && len(fields[len(fields)-1]) < 6 {
		return name
	}
	if groups >= len(fields) {
		return ""
	}
	return strings.Join(fields[:len(fields)-groups], " ")
}

func accountDigitGroup(value string) bool {
	digits := 0
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits++
			continue
		}
		if r != '-' && r != '.' && r != '*' {
			return false
		}
	}
	return digits > 0
}

func payeeMatchNames(raw string) []string {
	exact := lowerCollapse(raw)
	normalized := lowerCollapse(NormalizePayeeName(raw))
	if normalized == "" {
		return nil
	}
	if normalized == exact {
		return []string{exact}
	}
	return []string{exact, normalized}
}

func lowerCollapse(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func ExistingMerchantID(ctx context.Context, q MerchantQuerier, household, raw string) (string, error) {
	names := payeeMatchNames(raw)
	if len(names) == 0 {
		return "", nil
	}
	id, err := matchAliasMerchant(ctx, q, household, names)
	if err != nil || id != "" {
		return id, err
	}
	return matchNamedMerchant(ctx, q, household, names)
}

func matchAliasMerchant(ctx context.Context, q MerchantQuerier, household string, names []string) (string, error) {
	var count int
	var id *string
	if err := q.QueryRow(ctx, `SELECT count(DISTINCT ma.normalized_merchant_id),min(ma.normalized_merchant_id::text)
		FROM merchant_alias ma
		WHERE ma.household_id=$1 AND lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))=ANY($2::text[])`, household, names).Scan(&count, &id); err != nil {
		return "", err
	}
	if count > 1 {
		return "", ErrAmbiguousMerchantMatch
	}
	if count != 1 || id == nil {
		return "", nil
	}
	return *id, nil
}

func matchNamedMerchant(ctx context.Context, q MerchantQuerier, household string, names []string) (string, error) {
	var count int
	var id *string
	if err := q.QueryRow(ctx, `SELECT count(DISTINCT m.id),min(m.id::text)
		FROM merchant m
		WHERE m.household_id=$1 AND lower(regexp_replace(btrim(m.normalized_name),'[[:space:]]+',' ','g'))=ANY($2::text[])`, household, names).Scan(&count, &id); err != nil {
		return "", err
	}
	if count > 1 {
		return "", ErrAmbiguousMerchantMatch
	}
	if count != 1 || id == nil {
		return "", nil
	}
	return *id, nil
}

func ResolveOrCreateMerchant(ctx context.Context, q MerchantQuerier, household, raw string) (string, error) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", nil
	}
	if strings.TrimSpace(NormalizePayeeName(raw)) == "" {
		return "", ErrUnusableMerchantName
	}
	id, err := ExistingMerchantID(ctx, q, household, raw)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	err = q.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=merchant.updated_at RETURNING id`, household, raw).Scan(&id)
	return id, err
}

func MerchantLinkNeedsReview(ctx context.Context, q MerchantQuerier, household, raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	if strings.TrimSpace(NormalizePayeeName(raw)) == "" {
		return true, nil
	}
	_, err := ExistingMerchantID(ctx, q, household, raw)
	if errors.Is(err, ErrAmbiguousMerchantMatch) {
		return true, nil
	}
	return false, err
}

type MerchantMemoryMatch struct {
	MerchantID string
	CategoryID string
	Slug       string
}

func LookupMerchantMemory(ctx context.Context, q MerchantQuerier, household, raw string) (*MerchantMemoryMatch, error) {
	names := payeeMatchNames(raw)
	if len(names) == 0 {
		return nil, nil
	}
	var aliases, merchants, categories int
	var merchantID, categoryID, slug *string
	if err := q.QueryRow(ctx, `SELECT count(*),
		count(DISTINCT ma.normalized_merchant_id) FILTER (WHERE ma.auto_apply AND ma.created_from_user_confirmation AND c.id IS NOT NULL),
		count(DISTINCT ma.default_category_id) FILTER (WHERE ma.auto_apply AND ma.created_from_user_confirmation AND c.id IS NOT NULL),
		min(ma.normalized_merchant_id::text) FILTER (WHERE ma.auto_apply AND ma.created_from_user_confirmation AND c.id IS NOT NULL),
		min(c.id::text) FILTER (WHERE ma.auto_apply AND ma.created_from_user_confirmation AND c.id IS NOT NULL),
		min(c.slug) FILTER (WHERE ma.auto_apply AND ma.created_from_user_confirmation AND c.id IS NOT NULL)
		FROM merchant_alias ma
		LEFT JOIN category c ON c.id=ma.default_category_id AND c.household_id=ma.household_id AND c.active
		WHERE ma.household_id=$1 AND lower(regexp_replace(btrim(ma.raw_name),'[[:space:]]+',' ','g'))=ANY($2::text[])`, household, names).Scan(&aliases, &merchants, &categories, &merchantID, &categoryID, &slug); err != nil {
		return nil, err
	}
	if aliases == 0 || merchants != 1 || categories != 1 || merchantID == nil || categoryID == nil || slug == nil {
		return nil, nil
	}
	return &MerchantMemoryMatch{MerchantID: *merchantID, CategoryID: *categoryID, Slug: *slug}, nil
}
