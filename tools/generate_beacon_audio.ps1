param(
    [string]$OutputDirectory = (Join-Path (Get-Location) 'output\beacon-audio'),
    [string]$ChineseVoice = 'zh-CN-YunyangNeural',
    [string]$EnglishVoice = 'en-US-GuyNeural',
    [string]$PythonPath = 'python',
    [string]$FFmpegPath = 'ffmpeg'
)

$ErrorActionPreference = 'Stop'

$ffmpeg = (Get-Command $FFmpegPath -ErrorAction Stop).Source
$python = (Get-Command $PythonPath -ErrorAction Stop).Source
$workDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("draarl-beacon-audio-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $OutputDirectory, $workDirectory | Out-Null

$chineseTime = @(
    '凌晨零点整', '凌晨一点整', '凌晨两点整', '凌晨三点整',
    '凌晨四点整', '清晨五点整', '早上六点整', '早上七点整',
    '早上八点整', '上午九点整', '上午十点整', '上午十一点整',
    '中午十二点整', '下午一点整', '下午两点整', '下午三点整',
    '下午四点整', '下午五点整', '傍晚六点整', '晚上七点整',
    '晚上八点整', '晚上九点整', '晚上十点整', '晚上十一点整'
)
$englishTime = @(
    'It is midnight.',
    "It is one o'clock in the morning.",
    "It is two o'clock in the morning.",
    "It is three o'clock in the morning.",
    "It is four o'clock in the morning.",
    "It is five o'clock in the morning.",
    "It is six o'clock in the morning.",
    "It is seven o'clock in the morning.",
    "It is eight o'clock in the morning.",
    "It is nine o'clock in the morning.",
    "It is ten o'clock in the morning.",
    "It is eleven o'clock in the morning.",
    'It is noon.',
    "It is one o'clock in the afternoon.",
    "It is two o'clock in the afternoon.",
    "It is three o'clock in the afternoon.",
    "It is four o'clock in the afternoon.",
    "It is five o'clock in the afternoon.",
    "It is six o'clock in the evening.",
    "It is seven o'clock in the evening.",
    "It is eight o'clock in the evening.",
    "It is nine o'clock in the evening.",
    "It is ten o'clock in the evening.",
    "It is eleven o'clock in the evening."
)

function Write-EdgeSpeech {
    param(
        [string]$Text,
        [string]$Voice,
        [string]$Path
    )

    & $python '-m' 'edge_tts' '--voice' $Voice '--text' $Text '--write-media' $Path
    if ($LASTEXITCODE -ne 0) {
        throw "edge-tts failed for $Path"
    }
}

$backgroundPath = Join-Path $workDirectory 'background.wav'
$silencePath = Join-Path $workDirectory 'silence.wav'
$musicFilter = 'aevalsrc=0.035*sin(2*PI*220*t)*(0.7+0.3*sin(2*PI*0.08*t))+0.020*sin(2*PI*277.18*t)*(0.7+0.3*sin(2*PI*0.07*t))+0.012*sin(2*PI*329.63*t):s=16000:d=30,afade=t=in:st=0:d=1,afade=t=out:st=27:d=3'
& $ffmpeg @('-y', '-v', 'error', '-f', 'lavfi', '-i', $musicFilter, '-c:a', 'pcm_s16le', '-ar', '16000', '-ac', '1', $backgroundPath)
& $ffmpeg @('-y', '-v', 'error', '-f', 'lavfi', '-i', 'anullsrc=r=16000:cl=mono', '-t', '0.35', '-c:a', 'pcm_s16le', $silencePath)

for ($hour = 0; $hour -lt $chineseTime.Count; $hour++) {
    $hourCode = '{0:D2}' -f $hour
    $chinesePath = Join-Path $workDirectory "cn-$hourCode.mp3"
    $englishPath = Join-Path $workDirectory "en-$hourCode.mp3"
    $combinedPath = Join-Path $workDirectory "combined-$hourCode.wav"
    $outputPath = Join-Path $OutputDirectory "sanming-beacon-$hourCode.wav"

    Write-EdgeSpeech "这里是三明市业余无线电中继台，现在是$($chineseTime[$hour])。" $ChineseVoice $chinesePath
    Write-EdgeSpeech "This is the Sanming Amateur Radio Repeater. $($englishTime[$hour])" $EnglishVoice $englishPath

    & $ffmpeg @(
        '-y', '-v', 'error',
        '-i', $chinesePath,
        '-i', $silencePath,
        '-i', $englishPath,
        '-filter_complex', '[0:a]aresample=16000,aformat=sample_fmts=s16:sample_rates=16000:channel_layouts=mono[cn];[1:a]aresample=16000,aformat=sample_fmts=s16:sample_rates=16000:channel_layouts=mono[sil];[2:a]aresample=16000,aformat=sample_fmts=s16:sample_rates=16000:channel_layouts=mono[en];[cn][sil][en]concat=n=3:v=0:a=1',
        '-c:a', 'pcm_s16le', '-ar', '16000', '-ac', '1', $combinedPath
    )
    & $ffmpeg @(
        '-y', '-v', 'error',
        '-i', $combinedPath,
        '-i', $backgroundPath,
        '-filter_complex', '[1:a]volume=0.14[bg];[0:a][bg]amix=inputs=2:duration=first:dropout_transition=0,alimiter=limit=0.95',
        '-c:a', 'pcm_s16le', '-ar', '16000', '-ac', '1', $outputPath
    )
}

Get-ChildItem -LiteralPath $OutputDirectory -Filter 'sanming-beacon-*.wav' -File |
    Sort-Object Name |
    Select-Object Name, Length

[System.IO.Directory]::Delete($workDirectory, $true)
