// Shared domain types for the web app. These mirror the API responses.

export interface User {
  id: string;
  githubUserId: number;
  githubLogin: string;
  displayName: string;
  avatarUrl: string;
}

export interface ApiError {
  error: {
    code: string;
    message: string;
  };
}
