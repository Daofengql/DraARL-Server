<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const isLogin = ref(true)
const showPassword = ref(false)
const valid = ref(false)
const loading = ref(false)

const loginForm = ref({
  username: '',
  password: '',
})

const registerForm = ref({
  username: '',
  password: '',
  nickname: '',
})

const errorMessage = ref('')

// 当前表单的计算属性
const currentUsername = computed({
  get: () => isLogin.value ? loginForm.value.username : registerForm.value.username,
  set: (val) => {
    if (isLogin.value) {
      loginForm.value.username = val
    } else {
      registerForm.value.username = val
    }
  },
})

const currentPassword = computed({
  get: () => isLogin.value ? loginForm.value.password : registerForm.value.password,
  set: (val) => {
    if (isLogin.value) {
      loginForm.value.password = val
    } else {
      registerForm.value.password = val
    }
  },
})

async function handleSubmit() {
  if (!valid.value) return

  loading.value = true
  errorMessage.value = ''

  try {
    if (isLogin.value) {
      const result = await authStore.login(loginForm.value)
      if (result.success) {
        router.push('/')
      } else {
        errorMessage.value = result.message || '登录失败'
      }
    } else {
      const result = await authStore.register(registerForm.value)
      if (result.success) {
        // 注册成功后切换到登录
        isLogin.value = true
        loginForm.value.username = registerForm.value.username
        errorMessage.value = '注册成功，请登录'
      } else {
        errorMessage.value = result.message || '注册失败'
      }
    }
  } finally {
    loading.value = false
  }
}

function toggleMode() {
  isLogin.value = !isLogin.value
  errorMessage.value = ''
}

const usernameRules = [
  (v: string) => !!v || '请输入用户名',
  (v: string) => (v && v.length >= 3) || '用户名至少3个字符',
]

const passwordRules = [
  (v: string) => !!v || '请输入密码',
  (v: string) => (v && v.length >= 6) || '密码至少6个字符',
]
</script>

<template>
  <v-container class="fill-height bg-grey-lighten-4">
    <v-row justify="center" align="center">
      <v-col cols="12" sm="8" md="6" lg="4">
        <!-- Logo and Title -->
        <div class="text-center mb-8">
          <v-icon size="80" color="primary" class="mb-4">
            mdi-radio-handheld
          </v-icon>
          <h1 class="text-h4 font-weight-bold text-grey-darken-3">NRL 火链</h1>
          <p class="text-subtitle-1 text-grey-darken-1 mt-2">无线电网络互联系统</p>
        </div>

        <!-- Login/Register Card -->
        <v-card class="elevation-2 rounded-xl" border>
          <v-card-text class="pa-6">
            <v-form v-model="valid" @submit.prevent="handleSubmit">
              <!-- Username Field -->
              <v-text-field
                v-model="currentUsername"
                :rules="usernameRules"
                label="用户名"
                variant="outlined"
                prepend-inner-icon="mdi-account-outline"
                class="mb-2"
                required
              ></v-text-field>

              <!-- Nickname Field (Register Only) -->
              <v-text-field
                v-if="!isLogin"
                v-model="registerForm.nickname"
                label="昵称（可选）"
                variant="outlined"
                prepend-inner-icon="mdi-account-circle-outline"
                class="mb-2"
              ></v-text-field>

              <!-- Password Field -->
              <v-text-field
                v-model="currentPassword"
                :rules="passwordRules"
                :type="showPassword ? 'text' : 'password'"
                label="密码"
                variant="outlined"
                prepend-inner-icon="mdi-lock-outline"
                :append-inner-icon="showPassword ? 'mdi-eye' : 'mdi-eye-off'"
                @click:append-inner="showPassword = !showPassword"
                class="mb-4"
                required
              ></v-text-field>

              <!-- Error Message -->
              <v-alert
                v-if="errorMessage"
                :type="errorMessage.includes('成功') ? 'success' : 'error'"
                density="compact"
                class="mb-4"
                :closable="false"
              >
                {{ errorMessage }}
              </v-alert>

              <!-- Submit Button -->
              <v-btn
                type="submit"
                color="primary"
                size="large"
                block
                :loading="loading"
                :disabled="!valid"
                class="rounded-lg"
              >
                {{ isLogin ? '登录' : '注册' }}
              </v-btn>
            </v-form>
          </v-card-text>

          <!-- Toggle Login/Register -->
          <v-card-actions class="pa-4 pt-0">
            <v-spacer></v-spacer>
            <span class="text-grey-darken-1">
              {{ isLogin ? '还没有账号？' : '已有账号？' }}
            </span>
            <v-btn
              variant="text"
              color="primary"
              @click="toggleMode"
            >
              {{ isLogin ? '注册' : '登录' }}
            </v-btn>
          </v-card-actions>
        </v-card>

        <!-- Footer -->
        <p class="text-center text-caption text-grey-darken-1 mt-6">
          © {{ new Date().getFullYear() }} NRL 火链 · 无线电网络互联系统
        </p>
      </v-col>
    </v-row>
  </v-container>
</template>

<style scoped>
.fill-height {
  min-height: 100vh;
}
</style>
