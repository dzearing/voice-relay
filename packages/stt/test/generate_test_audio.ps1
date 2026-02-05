# Generate test audio files using Windows TTS
Add-Type -AssemblyName System.Speech

$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
$synth.Rate = 0  # Normal speed

$mediaDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$mediaDir = Join-Path $mediaDir "media"

$tests = @(
    @{ File = "hello_world.wav"; Text = "Hello world"; Rate = 0 },
    @{ File = "quick_brown_fox.wav"; Text = "The quick brown fox jumps over the lazy dog"; Rate = 0 },
    @{ File = "numbers.wav"; Text = "One two three four five"; Rate = 0 },
    @{ File = "filler_words.wav"; Text = "So um I was thinking that uh maybe we could like go to the store"; Rate = -1 }
)

foreach ($test in $tests) {
    $outputPath = Join-Path $mediaDir $test.File
    Write-Host "Generating: $($test.File)"
    Write-Host "  Text: $($test.Text)"

    $synth.Rate = $test.Rate
    $synth.SetOutputToWaveFile($outputPath)
    $synth.Speak($test.Text)
}

$synth.SetOutputToNull()
$synth.Dispose()

Write-Host "`nDone! Generated $($tests.Count) test files."
