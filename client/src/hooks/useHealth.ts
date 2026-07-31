import { useQuery } from "@tanstack/react-query";
import { healthService } from "../services/api/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";
import { config } from "../config/index.ts";

export function useHealth() {
  const query = useQuery({
    queryKey: QueryKeys.health,
    queryFn: () => healthService.fetchHealth(),
    refetchInterval: config.pollIntervalMs,
  });

  return { health: query.data ?? null, isLoading: query.isLoading, error: query.error };
}
