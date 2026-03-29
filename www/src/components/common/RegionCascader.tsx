import { useState, useEffect, useMemo } from 'react'
import {
  Box,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  type SelectChangeEvent,
  FormHelperText,
} from '@mui/material'

// 导入 china-division 数据
import citiesData from 'china-division/dist/cities.json'
import areasData from 'china-division/dist/areas.json'

// 省份数据（从 cities 中提取唯一的省份）
interface Province {
  code: string
  name: string
}

interface City {
  code: string
  name: string
  provinceCode: string
}

interface Area {
  code: string
  name: string
  cityCode: string
  provinceCode: string
}

// 构建省份数据
const provinces: Province[] = [
  { code: '11', name: '北京市' },
  { code: '12', name: '天津市' },
  { code: '13', name: '河北省' },
  { code: '14', name: '山西省' },
  { code: '15', name: '内蒙古自治区' },
  { code: '21', name: '辽宁省' },
  { code: '22', name: '吉林省' },
  { code: '23', name: '黑龙江省' },
  { code: '31', name: '上海市' },
  { code: '32', name: '江苏省' },
  { code: '33', name: '浙江省' },
  { code: '34', name: '安徽省' },
  { code: '35', name: '福建省' },
  { code: '36', name: '江西省' },
  { code: '37', name: '山东省' },
  { code: '41', name: '河南省' },
  { code: '42', name: '湖北省' },
  { code: '43', name: '湖南省' },
  { code: '44', name: '广东省' },
  { code: '45', name: '广西壮族自治区' },
  { code: '46', name: '海南省' },
  { code: '50', name: '重庆市' },
  { code: '51', name: '四川省' },
  { code: '52', name: '贵州省' },
  { code: '53', name: '云南省' },
  { code: '54', name: '西藏自治区' },
  { code: '61', name: '陕西省' },
  { code: '62', name: '甘肃省' },
  { code: '63', name: '青海省' },
  { code: '64', name: '宁夏回族自治区' },
  { code: '65', name: '新疆维吾尔自治区' },
  { code: '71', name: '台湾省' },
  { code: '81', name: '香港特别行政区' },
  { code: '82', name: '澳门特别行政区' },
]

const cities = citiesData as City[]
const areas = areasData as Area[]

interface RegionCascaderProps {
  value: string           // 完整位置字符串，如 "广东省 深圳市 南山区"
  onChange: (value: string) => void
  label?: string
  disabled?: boolean
  size?: 'small' | 'medium'
  required?: boolean
  helperText?: string
  error?: boolean
  fullWidth?: boolean
}

export function RegionCascader({
  value,
  onChange,
  label = '位置',
  disabled = false,
  size = 'medium',
  required = false,
  helperText,
  error = false,
  fullWidth = true,
}: RegionCascaderProps) {
  // 解析初始值
  const parseValue = (locationStr: string) => {
    if (!locationStr) return { provinceCode: '', cityCode: '', areaCode: '' }

    const parts = locationStr.split(' ').filter(Boolean)
    let provinceCode = ''
    let cityCode = ''
    let areaCode = ''

    // 匹配省份
    for (const p of provinces) {
      if (parts[0] && p.name === parts[0]) {
        provinceCode = p.code
        break
      }
    }

    // 匹配城市
    if (provinceCode && parts[1]) {
      const city = cities.find(c => c.provinceCode === provinceCode && c.name === parts[1])
      if (city) cityCode = city.code
    }

    // 匹配区县
    if (cityCode && parts[2]) {
      const area = areas.find(a => a.cityCode === cityCode && a.name === parts[2])
      if (area) areaCode = area.code
    }

    return { provinceCode, cityCode, areaCode }
  }

  const initial = parseValue(value)
  const [provinceCode, setProvinceCode] = useState(initial.provinceCode)
  const [cityCode, setCityCode] = useState(initial.cityCode)
  const [areaCode, setAreaCode] = useState(initial.areaCode)

  // 当外部 value 变化时同步状态
  useEffect(() => {
    const parsed = parseValue(value)
    setProvinceCode(parsed.provinceCode)
    setCityCode(parsed.cityCode)
    setAreaCode(parsed.areaCode)
  }, [value])

  // 获取当前省份的城市列表
  const cityOptions = useMemo(() => {
    if (!provinceCode) return []
    return cities.filter(c => c.provinceCode === provinceCode)
  }, [provinceCode])

  // 获取当前城市的区县列表
  const areaOptions = useMemo(() => {
    if (!cityCode) return []
    return areas.filter(a => a.cityCode === cityCode)
  }, [cityCode])

  // 构建完整位置字符串
  const buildLocationString = (pCode: string, cCode: string, aCode: string) => {
    const parts: string[] = []

    const province = provinces.find(p => p.code === pCode)
    if (province) parts.push(province.name)

    const city = cities.find(c => c.code === cCode)
    if (city) parts.push(city.name)

    const area = areas.find(a => a.code === aCode)
    if (area) parts.push(area.name)

    return parts.join(' ')
  }

  // 处理省份变化
  const handleProvinceChange = (event: SelectChangeEvent) => {
    const newProvinceCode = event.target.value
    setProvinceCode(newProvinceCode)
    setCityCode('')
    setAreaCode('')
    onChange(buildLocationString(newProvinceCode, '', ''))
  }

  // 处理城市变化
  const handleCityChange = (event: SelectChangeEvent) => {
    const newCityCode = event.target.value
    setCityCode(newCityCode)
    setAreaCode('')
    onChange(buildLocationString(provinceCode, newCityCode, ''))
  }

  // 处理区县变化
  const handleAreaChange = (event: SelectChangeEvent) => {
    const newAreaCode = event.target.value
    setAreaCode(newAreaCode)
    onChange(buildLocationString(provinceCode, cityCode, newAreaCode))
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, width: fullWidth ? '100%' : 'auto' }}>
      <Box sx={{ display: 'flex', gap: 1 }}>
        <FormControl size={size} sx={{ minWidth: 100, flex: 1 }} required={required} error={error}>
          <InputLabel shrink>{label}</InputLabel>
          <Select
            value={provinceCode}
            onChange={handleProvinceChange}
            label={`${label}${required ? ' *' : ''}`}
            disabled={disabled}
            displayEmpty
            notched
          >
            <MenuItem value="">
              <em>选择省份</em>
            </MenuItem>
            {provinces.map((p) => (
              <MenuItem key={p.code} value={p.code}>
                {p.name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControl
          size={size}
          sx={{ minWidth: 100, flex: 1 }}
          disabled={disabled || !provinceCode}
          required={required}
          error={error}
        >
          <InputLabel shrink>城市</InputLabel>
          <Select
            value={cityCode}
            onChange={handleCityChange}
            label="城市 *"
            displayEmpty
            notched
          >
            <MenuItem value="">
              <em>选择城市</em>
            </MenuItem>
            {cityOptions.map((c) => (
              <MenuItem key={c.code} value={c.code}>
                {c.name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControl
          size={size}
          sx={{ minWidth: 100, flex: 1 }}
          disabled={disabled || !cityCode}
        >
          <InputLabel shrink>区县</InputLabel>
          <Select
            value={areaCode}
            onChange={handleAreaChange}
            label="区县"
            displayEmpty
            notched
          >
            <MenuItem value="">
              <em>选择区县</em>
            </MenuItem>
            {areaOptions.map((a) => (
              <MenuItem key={a.code} value={a.code}>
                {a.name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>
      {helperText && (
        <FormHelperText error={error} sx={{ ml: 1.5 }}>
          {helperText}
        </FormHelperText>
      )}
    </Box>
  )
}
