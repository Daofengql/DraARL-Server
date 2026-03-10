<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from './stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const drawer = ref(false)
const userMenu = ref(false)

const navItems = computed(() => [
  { title: '首页', icon: 'mdi-home', to: '/' },
  { title: '设备管理', icon: 'mdi-radio-handheld', to: '/devices' },
  { title: '群组管理', icon: 'mdi-account-group', to: '/groups' },
  { title: '用户管理', icon: 'mdi-account-multiple', to: '/users', adminOnly: true },
  { title: '系统设置', icon: 'mdi-cog', to: '/settings', adminOnly: true },
])

const filteredNavItems = computed(() => {
  return navItems.value.filter(item => !item.adminOnly || authStore.isAdmin)
})

function navigateTo(to: string) {
  router.push(to)
  drawer.value = false
}

async function handleLogout() {
  await authStore.logout()
  router.push('/login')
}

onMounted(async () => {
  if (authStore.isAuthenticated) {
    await authStore.fetchUser()
  }
})
</script>

<template>
  <v-app class="bg-grey-lighten-4">
    <!-- Navigation Drawer -->
    <v-navigation-drawer
      v-model="drawer"
      temporary
      class="elevation-1"
    >
      <!-- User Profile Section -->
      <div class="pa-4 bg-primary">
        <div class="d-flex align-center">
          <v-avatar color="white" size="48" class="mr-3">
            <v-icon color="primary" size="28">mdi-account</v-icon>
          </v-avatar>
          <div class="text-white">
            <div class="text-subtitle-1 font-weight-medium">{{ authStore.user?.nickname || authStore.user?.username || '用户' }}</div>
            <div class="text-caption opacity-80">{{ authStore.isAdmin ? '管理员' : '普通用户' }}</div>
          </div>
        </div>
      </div>

      <v-list nav density="compact" class="pa-2">
        <v-list-item
          v-for="item in filteredNavItems"
          :key="item.to"
          :value="item.to"
          rounded="lg"
          @click="navigateTo(item.to)"
        >
          <template v-slot:prepend>
            <v-icon :icon="item.icon"></v-icon>
          </template>
          <v-list-item-title>{{ item.title }}</v-list-item-title>
        </v-list-item>
      </v-list>

      <template v-slot:append>
        <div class="pa-2">
          <v-list-item
            rounded="lg"
            @click="handleLogout"
          >
            <template v-slot:prepend>
              <v-icon>mdi-logout</v-icon>
            </template>
            <v-list-item-title>退出登录</v-list-item-title>
          </v-list-item>
        </div>
      </template>
    </v-navigation-drawer>

    <!-- App Bar -->
    <v-app-bar
      elevation="0"
      color="white"
      density="comfortable"
      class="border-b"
    >
      <v-app-bar-nav-icon
        variant="text"
        @click="drawer = !drawer"
      ></v-app-bar-nav-icon>

      <v-app-bar-title class="font-weight-medium">
        <span class="text-primary">NRL</span> 火链
      </v-app-bar-title>

      <v-spacer></v-spacer>

      <!-- User Menu -->
      <v-menu
        v-model="userMenu"
        :close-on-content-click="false"
        location="bottom end"
      >
        <template v-slot:activator="{ props }">
          <v-btn
            v-bind="props"
            variant="text"
            class="text-none"
          >
            <v-avatar size="32" color="primary-lighten-4" class="mr-2">
              <v-icon color="primary" size="20">mdi-account</v-icon>
            </v-avatar>
            <span class="text-subtitle-2">{{ authStore.user?.nickname || authStore.user?.username || '用户' }}</span>
            <v-icon end>mdi-chevron-down</v-icon>
          </v-btn>
        </template>

        <v-card min-width="200" rounded="lg" elevation="2">
          <v-list>
            <v-list-item>
              <v-list-item-title class="text-subtitle-2 font-weight-medium">
                {{ authStore.user?.nickname || authStore.user?.username }}
              </v-list-item-title>
              <v-list-item-subtitle class="text-caption">
                {{ authStore.isAdmin ? '管理员' : '普通用户' }}
              </v-list-item-subtitle>
            </v-list-item>
            <v-divider></v-divider>
            <v-list-item @click="handleLogout">
              <template v-slot:prepend>
                <v-icon>mdi-logout</v-icon>
              </template>
              <v-list-item-title>退出登录</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </v-app-bar>

    <!-- Main Content -->
    <v-main class="bg-grey-lighten-4">
      <router-view></router-view>
    </v-main>

    <!-- Footer -->
    <v-footer app class="bg-white border-t elevation-0">
      <v-row justify="center" no-gutters>
        <v-col class="text-center py-2" cols="12">
          <span class="text-caption text-grey-darken-1">
            © {{ new Date().getFullYear() }} NRL 火链 · 无线电网络互联系统
          </span>
        </v-col>
      </v-row>
    </v-footer>
  </v-app>
</template>

<style scoped>
.border-b {
  border-bottom: 1px solid rgba(0, 0, 0, 0.08) !important;
}

.border-t {
  border-top: 1px solid rgba(0, 0, 0, 0.08) !important;
}
</style>
