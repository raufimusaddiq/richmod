export const owner = (overrides = {}) => ({ id: "user-1", displayName: "Rafi", email: "rafi@example.test", isSuperAdmin: false, householdName: "Rumah Rafi", household: { role: "OWNER" }, ...overrides });
export const superAdmin = () => owner({ isSuperAdmin: true });

export const idr = value => new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(BigInt(value));

export const transaction = (id, merchant = "Pasar Minggu", overrides = {}) => ({
  id,
  type: "EXPENSE",
  status: "CONFIRMED",
  amount: "185000",
  merchantName: merchant,
  categoryName: "Belanja rumah",
  memberName: "Rafi",
  sourceType: "BANK_EMAIL",
  transactionAt: "2026-09-06T09:20:00+07:00",
  accountName: "Jago utama",
  ...overrides,
});

export const baseLists = {
  categories: () => [{ id: "cat-1", name: "Belanja rumah", active: true }],
  accounts: () => [{ id: "account-1", name: "Jago utama", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true }],
  members: () => [{ id: "member-1", displayName: "Rafi", role: "OWNER", active: true }],
};

export function ledgerBasics(path) {
  if (path === "/api/v1/categories") return baseLists.categories();
  if (path === "/api/v1/accounts") return baseLists.accounts();
  if (path === "/api/v1/household/members") return baseLists.members();
  return undefined;
}
