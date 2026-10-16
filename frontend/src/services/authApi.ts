import { request } from '@/lib/axios';
import type {
  LoginForm,
  RegisterForm,
  User,
  ApiResponse,
  UserProfile,
  ChangePasswordForm,
} from '@/types';

export const authApi = {
  login: async (loginData: LoginForm): Promise<ApiResponse<User>> => {
    return request<ApiResponse<User>>({
      method: 'post',
      url: '/auth/login',
      data: loginData,
    });
  },

  register: async (
    registerData: Omit<RegisterForm, 'confirmPassword' | 'agreement'>
  ): Promise<ApiResponse<User>> => {
    return request<ApiResponse<User>>({
      method: 'post',
      url: '/auth/register',
      data: registerData,
    });
  },

  logout: async (): Promise<ApiResponse<void>> => {
    return request<ApiResponse<void>>({
      method: 'post',
      url: '/auth/logout',
    });
  },

  getCurrentUser: async (): Promise<ApiResponse<User>> => {
    return request<ApiResponse<User>>({
      method: 'get',
      url: '/auth/user',
    });
  },

  updateProfile: async (profileData: UserProfile): Promise<ApiResponse<User>> => {
    return request<ApiResponse<User>>({
      method: 'put',
      url: '/auth/profile',
      data: profileData,
    });
  },

  changePassword: async (passwordData: ChangePasswordForm): Promise<ApiResponse<void>> => {
    return request<ApiResponse<void>>({
      method: 'put',
      url: '/auth/password',
      data: passwordData,
    });
  },

  refreshToken: async (): Promise<ApiResponse<{ token: string }>> => {
    return request<ApiResponse<{ token: string }>>({
      method: 'post',
      url: '/auth/refresh',
    });
  },
};
