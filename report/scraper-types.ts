export interface GitOpsSource {
  git: {
    url: string;
    branch: string;
    file: string;
    dir: string;
    link: string;
  };
  kustomize: {
    path: string;
    file: string;
  };
}

export interface ScraperInfo {
  id: string;
  name: string;
  namespace?: string;
  description?: string;
  source?: string;
  types: string[];
  specHash: string;
  createdBy?: string;
  createdAt: string;
  updatedAt?: string;
  gitops?: GitOpsSource;
}
