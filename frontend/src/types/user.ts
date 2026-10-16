export type UserRole = 'admin' | 'user';

export interface User {
  id: string;
  username: string;
  email: string;
  nickname: string;
  avatar?: string;
  role: UserRole;
  token?: string;
  created_at: string;
  last_login?: string;
  is_active: boolean;
}

export interface LoginForm {
  username: string;
  password: string;
  remember?: boolean;
}

export interface RegisterForm {
  username: string;
  email: string;
  password: string;
  confirmPassword: string;
  nickname: string;
  agreement: boolean;
}

export interface UserProfile {
  nickname: string;
  email: string;
  avatar?: string;
}

export interface ChangePasswordForm {
  old_password: string;
  new_password: string;
  confirm_password: string;
}
