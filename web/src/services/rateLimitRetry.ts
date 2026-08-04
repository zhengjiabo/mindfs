import { protectedJSON } from "./api";
import { appPath } from "./base";

export type RateLimitRetryPreferences = {
  enabled: boolean;
  base_delay_seconds: number;
  increment_seconds: number;
  recommended_base_delay_seconds?: number;
  last_recovery_seconds?: number;
  last_retry_count?: number;
  recovery_sample_count?: number;
};

export async function getRateLimitRetryPreferences(): Promise<RateLimitRetryPreferences> {
  return protectedJSON<RateLimitRetryPreferences>(appPath("/api/preferences/rate-limit-retry"));
}

export async function updateRateLimitRetryPreferences(
  preferences: Pick<RateLimitRetryPreferences, "enabled" | "base_delay_seconds" | "increment_seconds">,
): Promise<RateLimitRetryPreferences> {
  return protectedJSON<RateLimitRetryPreferences>(appPath("/api/preferences/rate-limit-retry"), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(preferences),
  });
}
