const RECOMMENDATION_ATTEMPTS = 3;
const MIN_RECOMMENDED_DELAY_SECONDS = 10;
const MAX_RECOMMENDED_DELAY_SECONDS = 3600;

export function recommendRateLimitRetryBaseDelay(
  recoverySeconds: number,
  probeSeconds: number,
  incrementSeconds: number,
): number {
  if (!Number.isFinite(recoverySeconds) || recoverySeconds <= 0) return 0;
  const probe = Math.max(0, Math.floor(Number.isFinite(probeSeconds) ? probeSeconds : 0));
  const increment = Math.max(0, Math.floor(Number.isFinite(incrementSeconds) ? incrementSeconds : 0));
  const incrementSteps = RECOMMENDATION_ATTEMPTS * (RECOMMENDATION_ATTEMPTS - 1) / 2;
  const remaining = Math.floor(recoverySeconds) - incrementSteps * increment - (RECOMMENDATION_ATTEMPTS - 1) * probe;
  return Math.max(
    MIN_RECOMMENDED_DELAY_SECONDS,
    Math.min(MAX_RECOMMENDED_DELAY_SECONDS, Math.floor(remaining / RECOMMENDATION_ATTEMPTS)),
  );
}

export function rateLimitRetryAttemptSchedule(
  baseDelaySeconds: number,
  incrementSeconds: number,
  probeSeconds: number,
): number[] {
  const schedule: number[] = [];
  let elapsed = 0;
  for (let attempt = 0; attempt < RECOMMENDATION_ATTEMPTS; attempt += 1) {
    if (attempt > 0) elapsed += Math.max(0, probeSeconds);
    elapsed += Math.max(0, baseDelaySeconds) + attempt * Math.max(0, incrementSeconds);
    schedule.push(elapsed);
  }
  return schedule;
}
