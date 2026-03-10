<script setup lang="ts">
import { ref } from 'vue'

const drawer = ref(false)
const items = ref([
  { title: '首页', icon: 'mdi-home', to: '/' },
  { title: '设备管理', icon: 'mdi-radio-handheld', to: '/devices' },
  { title: '群组管理', icon: 'mdi-account-group', to: '/groups' },
  { title: '用户管理', icon: 'mdi-account', to: '/users' },
  { title: '系统设置', icon: 'mdi-cog', to: '/settings' },
])
</script>

<template>
  <v-app>
    <v-navigation-drawer
      v-model="drawer"
      temporary
    >
      <v-list>
        <v-list-item
          v-for="(item, i) in items"
          :key="i"
          :value="item"
          active-color="primary"
        >
          <template v-slot:prepend>
            <v-icon :icon="item.icon"></v-icon>
          </template>
          <v-list-item-title v-text="item.title"></v-list-item-title>
        </v-list-item>
      </v-list>
    </v-navigation-drawer>

    <v-app-bar color="primary" density="compact">
      <v-app-bar-nav-icon @click="drawer = !drawer"></v-app-bar-nav-icon>
      <v-app-bar-title>NRL 火链 - 无线电网络互联系统</v-app-bar-title>
      <v-spacer></v-spacer>
      <v-btn icon="mdi-refresh"></v-btn>
    </v-app-bar>

    <v-main>
      <v-container fluid>
        <router-view></router-view>
      </v-container>
    </v-main>

    <v-footer app class="bg-grey-lighten-2">
      <v-row justify="center" no-gutters>
        <v-col class="text-center" cols="12">
          {{ new Date().getFullYear() }} — <strong>NRL 火链</strong>
        </v-col>
      </v-row>
    </v-footer>
  </v-app>
</template>

<style scoped>
</style>
