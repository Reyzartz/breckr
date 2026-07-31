import { useQuery } from "@tanstack/react-query";
import { healthService } from "../services/api/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";

export function useHealth() {
  const query = useQuery({
    queryKey: QueryKeys.health,
    queryFn: () => healthService.fetchHealth(),
  });

  return { health: query.data ?? null, isLoading: query.isLoading, error: query.error };
}
