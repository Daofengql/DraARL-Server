<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface Stats {
  onlineDevices: number
  totalDevices: number
  totalUsers: number
  totalGroups: number
  onlineUsers: number
}

const stats = ref<Stats>({
  onlineDevices: 0,
  totalDevices: 0,
  totalUsers: 0,
  totalGroups: 0,
  onlineUsers: 0,
})

const loading = ref(true)

onMounted(async () => {
  // TODO: 从API获取统计数据
  loading.value = false
})
</script>

<template>
  <v-container>
    <v-row>
      <v-col cols="12">
        <h1 class="text-h4 mb-4">NRL 火链 - 无线电网络互联系统</h1>
      </v-col>
    </v-row>

    <v-row v-if="!loading">
      <v-col cols="12" sm="6" md="3">
        <v-card class="mx-auto" prepend-icon="mdi-radio" title="在线设备">
          <v-card-text class="text-h4 text-center">{{ stats.onlineDevices }}</v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" sm="6" md="3">
        <v-card class="mx-auto" prepend-icon="mdi-radio-handheld" title="设备总数">
          <v-card-text class="text-h4 text-center">{{ stats.totalDevices }}</v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" sm="6" md="3">
        <v-card class="mx-auto" prepend-icon="mdi-account-group" title="群组数量">
          <v-card-text class="text-h4 text-center">{{ stats.totalGroups }}</v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" sm="6" md="3">
        <v-card class="mx-auto" prepend-icon="mdi-account" title="用户总数">
          <v-card-text class="text-h4 text-center">{{ stats.totalUsers }}</v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <v-row class="mt-4">
      <v-col cols="12">
        <v-card title="快速操作">
          <v-card-text>
            <v-row>
              <v-col cols="12" sm="6" md="3">
                <v-btn color="primary" block prepend-icon="mdi-plus" to="/devices">
                  添加设备
                </v-btn>
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-btn color="success" block prepend-icon="mdi-plus" to="/groups">
                  创建群组
                </v-btn>
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-btn color="info" block prepend-icon="mdi-account-plus" to="/users">
                  添加用户
                </v-btn>
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-btn color="warning" block prepend-icon="mdi-cog" to="/settings">
                  系统设置
                </v-btn>
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>
