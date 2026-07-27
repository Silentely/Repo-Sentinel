import { useQuery } from "@tanstack/react-query";

import { sessionQueryOptions } from "./api";

export function useSession() {
  return useQuery(sessionQueryOptions);
}
