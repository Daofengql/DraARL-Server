<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface Device {
  id: number
  name: string
  callsign: string
  ssid: number
  devModel: number
  groupID: number
  isOnline: boolean
  qth: string
}

const devices = ref<Device[]>([])
const loading = ref(true)
const search = ref('')

const headers = [
  { title: 'ID', key: 'id' },
  { title: '名称', key: 'name' },
  { title: '呼号', key: 'callsign' },
  { title: 'SSID', key: 'ssid' },
  { title: '型号', key: 'devModel' },
  { title: '群组', key: 'groupID' },
  { title: '状态', key: 'isOnline' },
  { title: '位置', key: 'qth' },
]

onMounted(async () => {
  // TODO: 从API获取设备列表
  loading.value = false
})
</script>

<template>
  <v-container>
    <v-row>
      <v-col cols="12">
        <v-card title="设备管理">
          <v-card-text>
            <v-text-field
              v-model="search"
              label="搜索"
              prepend-icon="mdi-magnify"
              variant="outlined"
              class="mb-4"
            ></v-text-field>

            <v-data-table
              :headers="headers"
              :items="devices"
              :search="search"
              :loading="loading"
            >
              <template v-slot:item.isOnline="{ item }">
                <v-chip :color="item.isOnline ? 'success' : 'grey'" size="small">
                  {{ item.isOnline ? '在线' : '离线' }}
                </v-chip>
              </template>
            </v-data-table>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>
