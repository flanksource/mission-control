import { fetchJSON } from "./http";

export type WhoAmIUser = {
  id: string;
  name?: string;
  email?: string;
  avatar?: string;
};

type WhoAmIResponse = {
  payload?: {
    user?: WhoAmIUser;
    roles?: string[];
  };
};

export async function getWhoAmI(): Promise<WhoAmIUser | null> {
  try {
    const result = await fetchJSON<WhoAmIResponse>("/auth/whoami");
    return result.payload?.user ?? null;
  } catch {
    return null;
  }
}
